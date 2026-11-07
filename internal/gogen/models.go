package gogen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/sqlgen"
)

// structs returns model/model.go: one struct per model. The auth model
// also holds the password hash, which JSON never shows.
func (g *gen) structs() string {
	var b strings.Builder
	b.WriteString(modelHeader)
	for _, m := range g.prog.Models {
		n := m.Name
		fmt.Fprintf(&b, "\n// %s is a record of model %s, declared at %s.\ntype %s struct {\n", n, n, where(m.Pos), n)
		for _, f := range structOrder(m) {
			fmt.Fprintf(&b, "\t%s %s `json:%q`\n", g.fields[f], strings.ReplaceAll(goType(f.Type), "model.", ""), f.Name)
		}
		if m.Auth {
			b.WriteString("\t// PasswordHash is the bcrypt hash of the password, or nil for an\n\t// account made without one, which cannot log in.\n\tPasswordHash *string `json:\"-\"`\n")
		}
		b.WriteString("}\n")
	}
	return b.String()
}

// models returns models.go: per model the functions that scan, check,
// find, insert, update and delete its records.
func (g *gen) models() string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("// tables returns the schema the runtime syncs at startup.\nfunc tables() []vibe.Table {\n\treturn []vibe.Table{\n")
	for _, t := range sqlgen.Schema(g.prog) {
		model := "login sessions"
		if t.Model != nil {
			model = t.Model.Name
		}
		fmt.Fprintf(&b, "\t\t{\n\t\t\tName: %q,\n\t\t\tModel: %q,\n\t\t\tCreate: %s,\n\t\t\tColumns: []vibe.Column{\n", t.Name, model, strconv.Quote(t.Create))
		for _, c := range t.Columns {
			field := c.Name
			if c.Field != nil {
				field = c.Field.Name
			}
			fmt.Fprintf(&b, "\t\t\t\t{Name: %q, Field: %q, Type: %q, NotNull: %t, Add: %s},\n", c.Name, field, c.Type, c.NotNull, strconv.Quote(c.Add))
		}
		b.WriteString("\t\t\t},\n")
		if len(t.Indexes) > 0 {
			b.WriteString("\t\t\tIndexes: []string{\n")
			for _, ix := range t.Indexes {
				fmt.Fprintf(&b, "\t\t\t\t%s,\n", strconv.Quote(ix))
			}
			b.WriteString("\t\t\t},\n")
		}
		b.WriteString("\t\t},\n")
	}
	b.WriteString("\t}\n}\n")
	for _, m := range g.prog.Models {
		g.model(&b, m)
	}
	return b.String()
}

// structOrder lists the fields of m as JSON shows them: id, the declared
// fields, then created_at and updated_at.
func structOrder(m *ir.Model) []*ir.Field {
	var first, last []*ir.Field
	for _, f := range m.Fields {
		if f.Implicit && f.Name != "id" {
			last = append(last, f)
		} else {
			first = append(first, f)
		}
	}
	return append(first, last...)
}

func (g *gen) model(b *strings.Builder, m *ir.Model) {
	n := m.Name
	dests := make([]string, len(m.Fields))
	args := make([]string, len(m.Fields))
	for i, f := range sqlgen.Columns(m) {
		x := "r." + g.fields[f]
		dests[i], args[i] = "&"+x, x
		switch {
		case f.Type.Kind == ir.Time:
			dests[i], args[i] = "vibe.ScanTime(&"+x+")", "vibe.TimeArg("+x+")"
		case f.Type.Kind == ir.Optional && f.Type.Elem.Kind == ir.Time:
			dests[i], args[i] = "vibe.ScanOptTime(&"+x+")", "vibe.OptTimeArg("+x+")"
		}
	}
	if m.Auth {
		dests, args = append(dests, "&r.PasswordHash"), append(args, "r.PasswordHash")
	}
	fmt.Fprintf(b, `
// scan%[1]s reads a %[1]s from a row whose columns are in table order.
func scan%[1]s(row vibe.Scanner) (*model.%[1]s, error) {
	r := &model.%[1]s{}
	if err := row.Scan(%[2]s); err != nil {
		return nil, err
	}
	return r, nil
}

// args%[1]s returns the columns of r in table order, as query parameters.
func args%[1]s(r *model.%[1]s) []any {
	return []any{%[3]s}
}
`, n, strings.Join(dests, ", "), strings.Join(args, ", "))

	g.check(b, m)

	for _, f := range m.Fields {
		if !f.Unique {
			continue
		}
		g.findFunc(b, m, f)
	}

	insert := g.constSQL("sql"+n+"Insert", sqlgen.Insert(m), "stores a new "+n+".")
	update := g.constSQL("sql"+n+"Update", sqlgen.Update(m), "writes a changed "+n+".")
	del := g.constSQL("sql"+n+"Delete", sqlgen.Delete(m), "removes a "+n+".")
	var slugs strings.Builder
	for _, f := range m.Fields {
		if f.Type.Kind != ir.Slug {
			continue
		}
		taken := g.constSQL("sql"+n+g.fields[f]+"Taken", sqlgen.Exists(m, f), "finds whether a "+n+" has a "+f.Name+".")
		source := "r." + g.fields[f.Source]
		if f.Source.Type.Kind == ir.Optional {
			source = "vibe.Deref(" + source + ")"
		}
		fmt.Fprintf(&slugs, `	if r.%[1]s == "" {
		s, err := vibe.UniqueSlug(%[2]s, %[3]q, func(s string) (bool, error) { return c.Exists(%[4]s, s) })
		if err != nil {
			return nil, err
		}
		r.%[1]s = s
	}
`, g.fields[f], source, m.Table, taken)
	}
	fmt.Fprintf(b, `
// insert%[1]s checks a new %[1]s, adding its problems to errs, and stores
// it.
func insert%[1]s(c *vibe.Ctx, r *model.%[1]s, errs *vibe.Errors) (*model.%[1]s, error) {
	now := vibe.Now()
	r.ID, r.CreatedAt, r.UpdatedAt = c.NewID(), now, now
	if err := check%[1]s(c, r, errs); err != nil {
		return nil, err
	}
	if err := errs.Err(); err != nil {
		return nil, err
	}
%[5]s	if err := c.Exec(%[2]s, args%[1]s(r)...); err != nil {
		return nil, err
	}
	return r, nil
}

// update%[1]s checks a changed %[1]s, adding its problems to errs, and
// stores it.
func update%[1]s(c *vibe.Ctx, r *model.%[1]s, errs *vibe.Errors) (*model.%[1]s, error) {
	r.UpdatedAt = vibe.Now()
	if err := check%[1]s(c, r, errs); err != nil {
		return nil, err
	}
	if err := errs.Err(); err != nil {
		return nil, err
	}
	if err := c.Exec(%[3]s, args%[1]s(r)...); err != nil {
		return nil, err
	}
	return r, nil
}

// delete%[1]s removes a %[1]s.
func delete%[1]s(c *vibe.Ctx, r *model.%[1]s) error {
	return c.Exec(%[4]s, r.ID)
}
`, n, insert, update, del, slugs.String())
}

// findFunc writes the function that loads a record of m by the unique
// field f.
func (g *gen) findFunc(b *strings.Builder, m *ir.Model, f *ir.Field) {
	n, fn := m.Name, g.fields[f]
	query := g.constSQL("sql"+n+"By"+fn, sqlgen.Find(m, f), "loads a "+n+" by "+f.Name+".")
	arg, guard := "v", ""
	switch elem(f.Type).Kind {
	case ir.ID:
		guard = fmt.Sprintf("\tif !vibe.ValidID(v) {\n\t\treturn nil, vibe.Missing(%q)\n\t}\n", n)
	case ir.Time:
		arg = "vibe.TimeArg(v)"
	}
	fmt.Fprintf(b, `
// find%[1]sBy%[2]s loads the %[1]s whose %[3]s is v, or answers not_found.
func find%[1]sBy%[2]s(c *vibe.Ctx, v %[4]s) (*model.%[1]s, error) {
%[5]s	return vibe.Find(c, %[6]s, scan%[1]s, %[1]q, %[7]s)
}
`, n, fn, f.Name, goType(elem(f.Type)), guard, query, arg)
}

// check writes the function that checks the field rules of a record and
// that its Refs exist.
func (g *gen) check(b *strings.Builder, m *ir.Model) {
	fmt.Fprintf(b, "\n// check%[1]s adds every rule r breaks to errs: the field rules of %[1]s\n// and the records its Refs name.\nfunc check%[1]s(c *vibe.Ctx, r *model.%[1]s, errs *vibe.Errors) error {\n", m.Name)
	for _, f := range m.Fields {
		if f.Implicit {
			continue
		}
		x := "r." + g.fields[f]
		rules := g.rules(f, x)
		if f.Type.Kind == ir.Optional && f.Type.Elem.Kind != ir.Record && rules != "" {
			rules = g.rules(f, "*"+x)
			fmt.Fprintf(b, "\tif %s != nil {\n%s\t}\n", x, indent(rules))
			continue
		}
		b.WriteString(rules)
	}
	for _, f := range m.Fields {
		if f.Ref == nil {
			continue
		}
		exists := g.constSQL("sql"+f.Ref.Name+"Exists", sqlgen.Exists(f.Ref, f.Ref.Field("id")), "finds whether a "+f.Ref.Name+" exists.")
		x := "r." + g.fields[f]
		check := fmt.Sprintf("\tif ok, err := c.Exists(%s, %s); err != nil {\n\t\treturn err\n\t} else if !ok {\n\t\terrs.Invalid(%q, %q)\n\t}\n", exists, x, f.Name, "no "+f.Ref.Name+" has this id")
		if f.Type.Kind == ir.Optional {
			check = fmt.Sprintf("\tif %s != nil {\n%s\t}\n", x, indent(strings.ReplaceAll(check, x, "*"+x)))
		}
		b.WriteString(check)
	}
	b.WriteString("\treturn nil\n}\n")
}

func indent(s string) string {
	lines := strings.SplitAfter(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "\t" + l
		}
	}
	return strings.Join(lines, "")
}

// rules returns the checks of the rules of field f on the value x.
func (g *gen) rules(f *ir.Field, x string) string {
	var b strings.Builder
	name := strconv.Quote(f.Name)
	t := elem(f.Type)
	switch {
	case t.IsText() && (f.Min != nil || f.Max != nil):
		var conds []string
		if f.Min != nil {
			n := f.Min.Value.(int64)
			conds = append(conds, fmt.Sprintf("n < %d {\n\t\terrs.Invalid(%s, %q)\n", n, name, "must be at least "+plural(n, "character")))
		}
		if f.Max != nil {
			n := f.Max.Value.(int64)
			conds = append(conds, fmt.Sprintf("n > %d {\n\t\terrs.Invalid(%s, %q)\n", n, name, "must be at most "+plural(n, "character")))
		}
		fmt.Fprintf(&b, "\tif n := vibe.Len(%s); %s\t}\n", x, strings.Join(conds, "\t} else if "))
	case t.IsNumeric():
		if f.Min != nil {
			fmt.Fprintf(&b, "\tif %s < %s {\n\t\terrs.Invalid(%s, %q)\n\t}\n", x, number(f.Min), name, "must be at least "+f.Min.String())
		}
		if f.Max != nil {
			fmt.Fprintf(&b, "\tif %s > %s {\n\t\terrs.Invalid(%s, %q)\n\t}\n", x, number(f.Max), name, "must be at most "+f.Max.String())
		}
	}
	switch t.Kind {
	case ir.Email:
		fmt.Fprintf(&b, "\tif !vibe.ValidEmail(%s) {\n\t\terrs.Invalid(%s, %q)\n\t}\n", x, name, "must be a valid email")
	case ir.Slug:
		fmt.Fprintf(&b, "\tif %s != \"\" && !vibe.ValidSlug(%s) {\n\t\terrs.Invalid(%s, %q)\n\t}\n", x, x, name, "must be lowercase letters, digits and dashes, like salam-dunya")
	}
	return b.String()
}

func plural(n int64, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.FormatInt(n, 10) + " " + word + "s"
}

// number returns a numeric constant as an untyped Go constant; Money is
// in minor units.
func number(k *ir.Const) string {
	switch v := k.Value.(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
	panic(fmt.Sprintf("gogen: bound of type %T", k.Value))
}
