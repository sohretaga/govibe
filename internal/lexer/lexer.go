package lexer

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/token"
)

// Result is the output of lexing one file.
type Result struct {
	Tokens   []token.Token // ends with EOF
	Comments []token.Token // Comment tokens in source order
	Diags    []diag.Diagnostic
}

// Lex converts the source of f into tokens.
func Lex(f *token.File) Result {
	l := &lexer{f: f, src: f.Source(), end: len(f.Source())}
	if strings.HasPrefix(string(l.src), "\uFEFF") {
		l.off = len("\uFEFF")
	}
	l.run()
	return Result{Tokens: l.toks, Comments: l.comments, Diags: l.diags}
}

type lexer struct {
	f        *token.File
	src      []byte
	off      int  // next byte to read
	end      int  // end of the range being lexed
	embedded bool // lexing the {expr} of a string or path

	toks     []token.Token
	comments []token.Token
	diags    []diag.Diagnostic
	open     []token.Kind // unclosed ( [ {
}

func (l *lexer) run() {
	for {
		l.skipSpace()
		if l.off >= l.end {
			break
		}
		c := l.src[l.off]
		switch {
		case c == '\n':
			l.newline()
		case c == '#':
			l.comment()
		case isLetter(c) || c == '_':
			l.word()
		case isDigit(c):
			l.number()
		case c == '"':
			l.str()
		case c == '/' && l.pathAllowed():
			l.path()
		default:
			l.operator()
		}
	}
	l.emit(token.EOF, l.end, l.end)
}

func (l *lexer) skipSpace() {
	for l.off < l.end {
		switch l.src[l.off] {
		case ' ', '\t', '\r':
			l.off++
		default:
			return
		}
	}
}

func (l *lexer) emit(k token.Kind, start, end int) {
	l.toks = append(l.toks, token.Token{
		Kind: k,
		Pos:  token.PosAt(start),
		End:  token.PosAt(end),
		Text: string(l.src[start:end]),
	})
}

func (l *lexer) emitText(k token.Kind, start, end int, text string) {
	l.emit(k, start, end)
	l.toks[len(l.toks)-1].Text = text
}

// report records a diagnostic and returns it so a fix can be attached.
// Past diag.Limit diagnostics it records nothing and returns nil: only the
// first ones are shown, and building more would cost time on long lines.
func (l *lexer) report(code diag.Code, start, end int, args ...any) *diag.Diagnostic {
	if len(l.diags) >= diag.Limit {
		return nil
	}
	l.diags = append(l.diags, diag.New(l.f, code, token.PosAt(start), token.PosAt(end), args...))
	return &l.diags[len(l.diags)-1]
}

func (l *lexer) fix(d *diag.Diagnostic, title string, start, end int, text string) {
	if d != nil {
		*d = d.WithFix(title, diag.Replace(l.f, token.PosAt(start), token.PosAt(end), text))
	}
}

// last returns the last emitted token, or an EOF token if there is none.
func (l *lexer) last() token.Token {
	if len(l.toks) == 0 {
		return token.Token{Kind: token.EOF}
	}
	return l.toks[len(l.toks)-1]
}

// lastIsMember reports whether the last token follows a '.', which makes
// even a keyword an ordinary member name.
func (l *lexer) lastIsMember() bool {
	return len(l.toks) >= 2 && l.toks[len(l.toks)-2].Kind == token.Dot
}

func (l *lexer) newline() {
	start := l.off
	l.off++
	if l.newlineIgnored() {
		return
	}
	l.emit(token.Newline, start, start+1)
}

// newlineIgnored applies the newline rules of the spec.
func (l *lexer) newlineIgnored() bool {
	if len(l.toks) == 0 {
		return true
	}
	if n := len(l.open); n > 0 && l.open[n-1] != token.LBrace {
		return true
	}
	last := l.last()
	switch {
	case last.Kind == token.Newline, last.Kind == token.Comma, last.Kind == token.Assign:
		return true
	case last.Kind.IsBinary():
		return !l.lastIsMember()
	}
	return false
}

func (l *lexer) comment() {
	start := l.off
	for l.off < l.end && l.src[l.off] != '\n' {
		l.off++
	}
	if l.embedded {
		l.report(diag.V104, start, start+1, "'#'", "a comment cannot start inside {...}")
		return
	}
	text := strings.TrimRight(string(l.src[start:l.off]), " \t\r")
	l.checkUTF8(start, l.off)
	l.comments = append(l.comments, token.Token{
		Kind: token.Comment,
		Pos:  token.PosAt(start),
		End:  token.PosAt(start + len(text)),
		Text: text,
	})
}

func (l *lexer) word() {
	start := l.off
	for l.off < l.end && isWordByte(l.src[l.off]) {
		l.off++
	}
	text := string(l.src[start:l.off])
	switch k := token.Lookup(text); {
	case k != token.Illegal:
		l.emit(k, start, l.off)
	case isUpper(text[0]):
		l.emit(token.TypeIdent, start, l.off)
	default:
		l.emit(token.Ident, start, l.off)
	}
}

func (l *lexer) number() {
	start := l.off
	l.digits()
	kind := token.Int
	if l.peek(0) == '.' && isDigit(l.peek(1)) {
		l.off++
		l.digits()
		kind = token.Float
	}
	reason := ""
	switch {
	case l.off < l.end && isWordByte(l.src[l.off]):
		for l.off < l.end && (isWordByte(l.src[l.off]) || l.src[l.off] == '.' && isDigit(l.peek(1))) {
			l.off++
		}
		reason = junkReason(string(l.src[start:l.off]))
	case l.peek(0) == '.' && isDigit(l.peek(1)):
		for l.off < l.end && (isDigit(l.src[l.off]) || l.src[l.off] == '_' || l.src[l.off] == '.' && isDigit(l.peek(1))) {
			l.off++
		}
		reason = "a number has at most one decimal point"
	case kind == token.Int && l.peek(0) == '.' && !isLetter(l.peek(1)) && l.peek(1) != '_':
		l.off++
		kind = token.Float
		reason = "a decimal point must be followed by digits"
	}
	text := string(l.src[start:l.off])
	if reason == "" {
		reason = numberReason(text, kind)
	}
	if reason != "" {
		l.report(diag.V106, start, l.off, quote(text), reason)
	}
	l.emit(kind, start, l.off)
}

func (l *lexer) digits() {
	for l.off < l.end && (isDigit(l.src[l.off]) || l.src[l.off] == '_') {
		l.off++
	}
}

func (l *lexer) peek(n int) byte {
	if l.off+n < l.end {
		return l.src[l.off+n]
	}
	return 0
}

func junkReason(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.HasPrefix(lower, "0x"), strings.HasPrefix(lower, "0b"), strings.HasPrefix(lower, "0o"):
		return "only decimal numbers are supported"
	case strings.ContainsAny(lower, "e") && strings.Trim(lower, "0123456789_.e") == "":
		return "exponents are not supported"
	}
	return "a number cannot be followed by letters"
}

// numberReason checks the '_' separators and the range of a well-formed
// number and returns why it is invalid, or "".
func numberReason(text string, kind token.Kind) string {
	for i := 0; i < len(text); i++ {
		if text[i] != '_' {
			continue
		}
		if i == 0 || i == len(text)-1 || !isDigit(text[i-1]) || !isDigit(text[i+1]) {
			return "'_' must stand between two digits"
		}
	}
	plain := strings.ReplaceAll(text, "_", "")
	if kind == token.Int {
		if _, err := strconv.ParseInt(plain, 10, 64); err != nil {
			return "it does not fit in a 64-bit integer"
		}
		return ""
	}
	if _, err := strconv.ParseFloat(plain, 64); err != nil {
		return "it is too large"
	}
	return ""
}

// str lexes a double-quoted string with escapes and {expr} interpolation.
func (l *lexer) str() {
	start := l.off
	l.off++ // opening quote
	var parts []token.Part
	var text strings.Builder
	textStart := l.off
	flush := func() {
		if l.off > textStart || text.Len() > 0 {
			parts = append(parts, token.Part{Pos: token.PosAt(textStart), End: token.PosAt(l.off), Text: text.String()})
		}
		text.Reset()
	}
	for {
		if l.atLineEnd() {
			flush()
			d := l.report(diag.V102, start, l.off, "add '\"' before the end of the line")
			l.fix(d, "Close the string", l.off, l.off, "\"")
			l.emitString(start, parts)
			return
		}
		switch c := l.src[l.off]; c {
		case '"':
			flush()
			l.off++
			l.emitString(start, parts)
			return
		case '\\':
			l.escape(&text)
		case '{':
			flush()
			closing, ok := scanBraces(l.src, l.off, l.end)
			if !ok {
				l.report(diag.V102, l.off, l.lineEnd(), "the interpolation '{' is not closed with '}'; write \\{ for a literal brace")
				l.off = l.lineEnd()
				l.emitString(start, parts)
				return
			}
			parts = append(parts, l.embed(l.off+1, closing))
			l.off = closing + 1
			textStart = l.off
		default:
			r, size := utf8.DecodeRune(l.src[l.off:l.end])
			switch {
			case r == utf8.RuneError && size == 1:
				l.report(diag.V104, l.off, l.off+1, "byte "+hexByte(c), "source text must be UTF-8")
			case r < ' ' && r != '\t':
				l.report(diag.V104, l.off, l.off+size, "U+"+strconv.FormatInt(int64(r), 16), "control characters cannot appear in a string")
			}
			text.WriteRune(r)
			l.off += size
		}
	}
}

func (l *lexer) emitString(start int, parts []token.Part) {
	l.emit(token.String, start, l.off)
	l.toks[len(l.toks)-1].Parts = parts
}

func (l *lexer) escape(text *strings.Builder) {
	start := l.off
	l.off++ // backslash
	if l.atLineEnd() {
		l.report(diag.V103, start, l.off, "'\\'")
		return
	}
	r, size := utf8.DecodeRune(l.src[l.off:l.end])
	l.off += size
	switch r {
	case 'n':
		text.WriteByte('\n')
	case 't':
		text.WriteByte('\t')
	case '"', '\\', '{':
		text.WriteRune(r)
	default:
		d := l.report(diag.V103, start, l.off, quote(string(l.src[start:l.off])))
		l.fix(d, "Remove the backslash", start, start+1, "")
		text.WriteRune(r)
	}
}

func (l *lexer) atLineEnd() bool {
	return l.off >= l.end || l.src[l.off] == '\n' || l.src[l.off] == '\r' && l.peek(1) == '\n'
}

// lineEnd returns the offset of the end of the current line, before any
// line ending.
func (l *lexer) lineEnd() int {
	i := l.off
	for i < l.end && l.src[i] != '\n' {
		i++
	}
	if i > l.off && l.src[i-1] == '\r' {
		i--
	}
	return i
}

// embed lexes the {expr} between start and end into an Expr part.
func (l *lexer) embed(start, end int) token.Part {
	sub := &lexer{f: l.f, src: l.src, off: start, end: end, embedded: true}
	sub.run()
	l.diags = append(l.diags, sub.diags[:min(len(sub.diags), max(diag.Limit-len(l.diags), 0))]...)
	return token.Part{Pos: token.PosAt(start), End: token.PosAt(end), Expr: true, Tokens: sub.toks}
}

// scanBraces returns the offset of the '}' that closes the '{' at open,
// skipping nested braces and strings. It fails at a line end.
func scanBraces(src []byte, open, end int) (int, bool) {
	depth := 0
	for i := open; i < end; i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		case '"':
			j, ok := skipString(src, i, end)
			if !ok {
				return 0, false
			}
			i = j - 1
		case '\n':
			return 0, false
		}
	}
	return 0, false
}

// skipString returns the offset after the string that starts at the quote
// at open.
func skipString(src []byte, open, end int) (int, bool) {
	for i := open + 1; i < end; i++ {
		switch src[i] {
		case '\\':
			i++
		case '"':
			return i + 1, true
		case '{':
			j, ok := scanBraces(src, i, end)
			if !ok {
				return 0, false
			}
			i = j
		case '\n':
			return 0, false
		}
	}
	return 0, false
}

func (l *lexer) pathAllowed() bool {
	last := l.last()
	return (last.Kind.IsMethod() || last.Kind == token.At) && !l.lastIsMember()
}

// path lexes a route path and validates its segments.
func (l *lexer) path() {
	start := l.off
	stop, unclosed := l.pathExtent()
	var parts []token.Part
	i := start + 1
	for stop > start+1 {
		segStart := i
		var params [][2]int
		for i < stop && l.src[i] != '/' {
			if l.src[i] == '{' {
				closing, ok := scanBraces(l.src, i, stop)
				if !ok {
					i = stop
					break
				}
				params = append(params, [2]int{i, closing})
				i = closing + 1
				continue
			}
			i++
		}
		if part, ok := l.segment(start, stop, segStart, i, params, unclosed); ok {
			parts = append(parts, part)
		}
		if i >= stop {
			break
		}
		i++ // '/'
		if i == stop {
			d := l.report(diag.V109, start, stop, quote(string(l.src[start:stop])), "remove the trailing '/'")
			l.fix(d, "Remove the trailing '/'", stop-1, stop, "")
			break
		}
	}
	l.off = stop
	l.emit(token.Path, start, stop)
	l.toks[len(l.toks)-1].Parts = parts
}

// pathExtent returns the end of the path at l.off and the offset of a '{'
// that is never closed, or -1.
func (l *lexer) pathExtent() (stop, unclosed int) {
	i := l.off
	for i < l.end {
		c := l.src[i]
		if c == '{' {
			closing, ok := scanBraces(l.src, i, l.end)
			if !ok {
				l.report(diag.V109, l.off, l.lineEnd(), quote(string(l.src[l.off:l.lineEnd()])), "'{' is not closed with '}'")
				return l.lineEnd(), i
			}
			i = closing + 1
			continue
		}
		if c == '?' {
			// A query string is invalid in a path; take all of it so that
			// the fix removes it whole.
			for i < l.end && !isSpace(l.src[i]) && l.src[i] != '#' {
				i++
			}
			break
		}
		if isPathStop(c) {
			break
		}
		_, size := utf8.DecodeRune(l.src[i:l.end])
		i += size
	}
	return i, -1
}

func isPathStop(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '(', ')', '[', ']', ',', '=', '#', '"', '}', ';':
		return true
	}
	return false
}

// segment validates one path segment between from and to and returns its
// part.
func (l *lexer) segment(start, stop, from, to int, params [][2]int, unclosed int) (token.Part, bool) {
	path := quote(string(l.src[start:stop]))
	seg := string(l.src[from:to])
	switch {
	case unclosed >= from && unclosed < to:
		return token.Part{}, false
	case seg == "":
		d := l.report(diag.V109, start, stop, path, "it has an empty segment")
		l.fix(d, "Remove the extra '/'", from-1, from, "")
		return token.Part{}, false
	case len(params) == 1 && params[0][0] == from && params[0][1] == to-1:
		return l.embed(from+1, to-1), true
	case len(params) > 0:
		l.report(diag.V109, start, stop, path, "segment "+quote(seg)+" mixes text and a parameter; a parameter fills a whole segment")
		return token.Part{}, false
	}
	if q := strings.IndexByte(seg, '?'); q >= 0 {
		d := l.report(diag.V109, start, stop, path, "a path has no query string; declare query values as route parameters")
		l.fix(d, "Remove the query string", from+q, stop, "")
		return token.Part{}, false
	}
	if seg[0] == ':' && isSnakeName(seg[1:]) {
		d := l.report(diag.V109, start, stop, path, "write a path parameter as {"+seg[1:]+"}")
		l.fix(d, "Use {"+seg[1:]+"}", from, to, "{"+seg[1:]+"}")
		return token.Part{}, false
	}
	if !isSegment(seg) {
		d := l.report(diag.V109, start, stop, path, "segment "+quote(seg)+" may only use lowercase letters, digits, '_' and '-'")
		if lower := strings.ToLower(seg); isSegment(lower) {
			l.fix(d, "Use "+quote(lower), from, to, lower)
		}
		return token.Part{}, false
	}
	return token.Part{Pos: token.PosAt(from), End: token.PosAt(to), Text: seg}, true
}

func (l *lexer) operator() {
	start := l.off
	c := l.src[l.off]
	two := func(next byte, double, single token.Kind) {
		if l.peek(1) == next {
			l.off += 2
			l.emit(double, start, l.off)
			return
		}
		l.off++
		l.emit(single, start, l.off)
	}
	switch c {
	case '+':
		l.single(token.Plus)
	case '-':
		two('>', token.Arrow, token.Minus)
	case '*':
		l.single(token.Star)
	case '/':
		if l.peek(1) == '/' {
			d := l.report(diag.V104, start, start+2, "'//'", "comments start with '#'")
			l.fix(d, "Use '#'", start, start+2, "#")
			l.off = l.lineEnd()
			return
		}
		l.single(token.Slash)
	case '%':
		l.single(token.Percent)
	case '=':
		two('=', token.Eq, token.Assign)
	case '<':
		two('=', token.LessEq, token.Less)
	case '>':
		two('=', token.GreaterEq, token.Greater)
	case '?':
		l.single(token.Question)
	case '.':
		l.single(token.Dot)
	case ',':
		l.single(token.Comma)
	case ':':
		l.single(token.Colon)
	case '(':
		l.single(token.LParen)
		l.open = append(l.open, token.LParen)
	case '[':
		l.single(token.LBrack)
		l.open = append(l.open, token.LBrack)
	case '{':
		l.single(token.LBrace)
		l.open = append(l.open, token.LBrace)
	case ')':
		l.single(token.RParen)
		l.close(token.LParen)
	case ']':
		l.single(token.RBrack)
		l.close(token.LBrack)
	case '}':
		l.single(token.RBrace)
		// A '}' also closes any '(' or '[' left open inside its block.
		for len(l.open) > 0 {
			top := l.open[len(l.open)-1]
			l.open = l.open[:len(l.open)-1]
			if top == token.LBrace {
				break
			}
		}
	case '!':
		if l.peek(1) == '=' {
			l.off += 2
			l.emit(token.NotEq, start, l.off)
			return
		}
		l.substitute(1, "'!'", "not", token.Not)
	case '&':
		if l.peek(1) == '&' {
			l.substitute(2, "'&&'", "and", token.And)
			return
		}
		l.substitute(1, "'&'", "and", token.And)
	case '|':
		if l.peek(1) == '|' {
			l.substitute(2, "'||'", "or", token.Or)
			return
		}
		l.substitute(1, "'|'", "or", token.Or)
	case ';':
		l.off++
		d := l.report(diag.V104, start, l.off, "';'", "statements end at a newline")
		if d == nil {
			return
		}
		if rest := strings.TrimSpace(string(l.src[l.off:l.lineEnd()])); rest == "" || rest[0] == '#' {
			l.fix(d, "Remove ';'", start, l.off, "")
		} else {
			l.fix(d, "Start a new line", start, l.off, "\n")
		}
	case '\'':
		l.singleQuoted()
	default:
		l.invalidRune()
	}
}

func (l *lexer) single(k token.Kind) {
	l.off++
	l.emit(k, l.off-1, l.off)
}

func (l *lexer) close(k token.Kind) {
	if n := len(l.open); n > 0 && l.open[n-1] == k {
		l.open = l.open[:n-1]
	}
}

// substitute reports an operator from another language and emits the
// GoVibe keyword with the same meaning, so parsing can continue.
func (l *lexer) substitute(width int, found, keyword string, k token.Kind) {
	start := l.off
	l.off += width
	d := l.report(diag.V104, start, l.off, found, "use '"+keyword+"'")
	text := keyword
	if k == token.Not {
		text += " "
	}
	l.fix(d, "Use '"+keyword+"'", start, l.off, text)
	l.emitText(k, start, l.off, keyword)
}

func (l *lexer) singleQuoted() {
	start := l.off
	closing := start + 1
	for closing < l.end && l.src[closing] != '\'' && l.src[closing] != '\n' {
		closing++
	}
	if closing >= l.end || l.src[closing] != '\'' {
		l.off++
		l.report(diag.V104, start, l.off, "\"'\"", "strings use double quotes")
		return
	}
	l.off = closing + 1
	content := string(l.src[start+1 : l.off-1])
	d := l.report(diag.V104, start, l.off, "\"'\"", "strings use double quotes")
	// Only the quotes change, so {expr} in the text stays an interpolation.
	l.fix(d, "Use double quotes", start, l.off, `"`+strings.ReplaceAll(content, `"`, `\"`)+`"`)
	l.emit(token.String, start, l.off)
	l.toks[len(l.toks)-1].Parts = []token.Part{{Pos: token.PosAt(start + 1), End: token.PosAt(l.off - 1), Text: content}}
}

func (l *lexer) invalidRune() {
	start := l.off
	r, size := utf8.DecodeRune(l.src[l.off:l.end])
	l.off += size
	switch {
	case r == utf8.RuneError && size == 1:
		l.report(diag.V104, start, l.off, "byte "+hexByte(l.src[start]), "source text must be UTF-8")
	case unicode.IsLetter(r):
		l.report(diag.V104, start, l.off, quote(string(r)), "names use only ASCII letters, digits and '_'")
	default:
		l.report(diag.V104, start, l.off, quote(string(r)), "it cannot start a token")
	}
}

func (l *lexer) checkUTF8(start, end int) {
	for i := start; i < end; {
		r, size := utf8.DecodeRune(l.src[i:end])
		if r == utf8.RuneError && size == 1 {
			l.report(diag.V104, i, i+1, "byte "+hexByte(l.src[i]), "source text must be UTF-8")
			return
		}
		i += size
	}
}

func isSpace(c byte) bool    { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
func isLetter(c byte) bool   { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
func isDigit(c byte) bool    { return '0' <= c && c <= '9' }
func isUpper(c byte) bool    { return 'A' <= c && c <= 'Z' }
func isWordByte(c byte) bool { return isLetter(c) || isDigit(c) || c == '_' }

func isSnakeName(s string) bool {
	if s == "" || isDigit(s[0]) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !('a' <= c && c <= 'z' || isDigit(c) || c == '_') {
			return false
		}
	}
	return true
}

func isSegment(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; !('a' <= c && c <= 'z' || isDigit(c) || c == '_' || c == '-') {
			return false
		}
	}
	return s != ""
}

func quote(s string) string { return "'" + s + "'" }

func hexByte(c byte) string {
	const digits = "0123456789abcdef"
	return "0x" + string([]byte{digits[c>>4], digits[c&15]})
}

// Quote returns s as a GoVibe string literal, escaping backslashes,
// quotes, braces, newlines and tabs.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '{':
			b.WriteString(`\{`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
