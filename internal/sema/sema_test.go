package sema

import (
	"os"
	"strings"
	"testing"

	"github.com/sohretaga/govibe/internal/ast"
	_ "github.com/sohretaga/govibe/internal/golden"
	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/parser"
	"github.com/sohretaga/govibe/internal/token"
)

func check(t *testing.T, sources ...string) *ir.Program {
	t.Helper()
	var files []*ast.File
	for i, src := range sources {
		name := "f" + string(rune('a'+i)) + ".vibe"
		file, diags := parser.ParseFile(token.NewFile(name, []byte(src)))
		for _, d := range diags {
			t.Fatalf("syntax: %v", d)
		}
		files = append(files, file)
	}
	prog, diags := Check(files)
	for _, d := range diags {
		t.Errorf("%v", d)
	}
	return prog
}

// shop is a valid program that uses every part of the v0.1 language.
const shop = `app Shop {
  db "sqlite://shop.db"
}

auth User

model User {
  name: Text min 2 max 120
  email: Email unique
  nickname: Text?
}

model Post {
  title: Text min 3 max 200
  slug: Slug(title)
  body: Text = ""
  author: Ref User
  score: Float = 0
  views: Int = 0
  price: Money min 0 = 0
  published: Bool = false
  reviewer: Ref User?
  published_at: Time?
}

extern fn charge_card(amount: Money, token: Text) -> Text

fn excerpt(text: Text, n: Int = 20) -> Text {
  if len(text) <= n { return text }
  return "{text.trim()}..."
}

fn weight(views: Int, score: Float) -> Float {
  let base = views * 2 + 1
  return base * score / 3 + 0.5
}

feature Blog {
  crud Post at /posts/{slug} owner author

  crud User at /users only get

  get /posts/search (q: Text?, min_score: Float?, limit = 20, offset = 0) = Post.where(title contains q, score >= min_score, published).order(-score).newest().limit(limit).offset(offset)

  get /posts/{slug}/excerpt = local_excerpt(Post.find(slug: slug).body)

  get /posts/count = Post.where(published, not (views > 100) or price < 5).count()

  get /posts/top = Post.all().order(views).first()

  post /posts/{post_id}/publish {
    let item = Post.find(post_id)
    if item.author != me.id { fail forbidden "not your post" }
    if item.published { fail conflict "already published" }
    return item.update(published: true, published_at: now(), score: item.score + 1)
  }

  patch /posts/{post_id}/price (price: Money, discount: Money?) {
    let item = Post.find(post_id)
    if price > 1000 or price * 2 < item.price - 1 { fail invalid "price out of range" }
    if discount != none {
      return item.update(price: price - discount)
    }
    return item.update(price: price * 3)
  }

  post /posts/{post_id}/review {
    let item = Post.find(post_id)
    let reviewer = item.reviewer
    if reviewer != none and reviewer == me.id {
      return "you reviewed it"
    } else if me.nickname != none {
      return "hello {me.name}"
    } else {
      return "thanks, {me.name}"
    }
  }

  post /pay (amount: Money, token: Text) = charge_card(amount, token)

  delete /posts/{post_id}/draft {
    let item = Post.find(post_id)
    item.delete()
  }

  get /tags = ["go", "vibe", excerpt("long text", 3)]

  fn local_excerpt(text: Text) -> Text {
    return excerpt(text: text, n: 10)
  }

  test "posts start unpublished" {
    let u = User.add(name: "Ursula", email: "u@example.com")
    login_as(u)
    let res = post /posts (title: "Hello world", price: 9.99)
    expect res.status == created
    expect res.body.published == false
    expect res.body.slug == "hello-world"
    expect (get /posts/{res.body.slug}).status == ok
    expect (post /posts/{res.body.id}/publish).status != conflict
    let p = Post.add(title: "Second", author: u.id, price: 7.50, score: 4)
    expect p.score > 3.5
    expect Post.where(author == u.id).count() == 2
    expect (delete /posts/{p.id}).status == empty
    expect len(Post.all()) >= 1
  }
}
`

func TestValidProgram(t *testing.T) {
	prog := check(t, shop)
	if prog.Auth == nil || prog.Auth.Name != "User" {
		t.Errorf("auth = %v", prog.Auth)
	}
}

func TestReferenceApp(t *testing.T) {
	src, err := os.ReadFile("../../examples/library/main.vibe")
	if err != nil {
		t.Fatal(err)
	}
	prog := check(t, string(src))
	var routes []string
	for _, r := range prog.Routes {
		login := ""
		if r.Login {
			login = " login"
		}
		routes = append(routes, r.Method+" "+r.Path+" -> "+r.Status+" "+r.Result.String()+login)
	}
	want := []string{
		"POST /auth/register -> created {user: User, token: Text}",
		"POST /auth/login -> ok {user: User, token: Text}",
		"POST /auth/logout -> empty no value",
		"GET /auth/me -> ok User login",
		"GET /books -> ok [Book]",
		"GET /books/{id} -> ok Book",
		"POST /books -> created Book login",
		"PATCH /books/{id} -> ok Book login",
		"DELETE /books/{id} -> empty no value login",
		"GET /books/search -> ok [Book]",
		"POST /books/{id}/publish -> ok Book login",
		"GET /healthz -> ok Json",
	}
	if strings.Join(routes, "\n") != strings.Join(want, "\n") {
		t.Errorf("routes:\n%s\nwant:\n%s", strings.Join(routes, "\n"), strings.Join(want, "\n"))
	}
	// Eleven endpoints, four auth, five crud and two custom, plus /healthz.
	if len(prog.Routes) != 12 {
		t.Errorf("%d routes, want 12", len(prog.Routes))
	}
	create := prog.Routes[6]
	var params []string
	for _, p := range create.Params {
		params = append(params, p.Name+": "+p.Type.String())
	}
	if got := strings.Join(params, ", "); got != "title: Text, pages: Int, price: Money, published: Bool?" {
		t.Errorf("create params = %s; the owner field seller must not be writable", got)
	}
}

func TestSplitFiles(t *testing.T) {
	check(t,
		"app Shop {}\n\nauth User\n",
		"model User {\n  email: Email unique\n}\n\nfn shout(t: Text) -> Text {\n  return t.upper()\n}\n",
		"model Order {\n  buyer: Ref User\n  note: Text\n}\n\nfeature Orders {\n  crud Order at /orders owner buyer private\n\n  get /orders/{id}/note = shout(Order.find(id).note)\n}\n",
	)
}

func TestConversions(t *testing.T) {
	tests := []struct {
		name, body string
		ok         bool
	}{
		{"int widens to float", "fn f(a: Int, b: Float) -> Float {\n  return a + b\n}", true},
		{"int fills a float", "fn f(a: Int) -> Float {\n  return a\n}", true},
		{"float does not fill an int", "fn f(a: Float) -> Int {\n  return a\n}", false},
		{"money plus money", "fn f(a: Money, b: Money) -> Money {\n  return a + b - 1\n}", true},
		{"money times int", "fn f(a: Money, n: Int) -> Money {\n  return n * a * 2\n}", true},
		{"money times float", "fn f(a: Money, x: Float) -> Money {\n  return a * x\n}", false},
		{"money divided", "fn f(a: Money) -> Money {\n  return a / 2\n}", false},
		{"money plus int", "fn f(a: Money, n: Int) -> Money {\n  return a + n\n}", false},
		{"money literal", "fn f(a: Money) -> Bool {\n  return a > 9.99\n}", true},
		{"money literal precision", "fn f(a: Money) -> Bool {\n  return a > 9.999\n}", false},
		{"int modulo", "fn f(a: Int) -> Int {\n  return a % 2\n}", true},
		{"float modulo", "fn f(a: Float) -> Float {\n  return a % 2\n}", false},
		{"email is text", "fn f(e: Email) -> Text {\n  return e.lower()\n}", true},
		{"text fills email", "fn f(t: Text) -> Email {\n  return t\n}", true},
		{"id is not text", "fn f(t: Text) -> ID {\n  return t\n}", false},
		{"text is not id", "fn f(i: ID) -> Text {\n  return i\n}", false},
		{"text plus text", "fn f(a: Text) -> Text {\n  return a + \"!\"\n}", false},
		{"optional needs a check", "fn f(a: Int?) -> Int {\n  return a\n}", false},
		{"narrowed optional", "fn f(a: Int?) -> Int {\n  if a != none { return a }\n  return 0\n}", true},
		{"narrowing ends with the block", "fn f(a: Int?) -> Int {\n  if a != none { return a }\n  return a\n}", false},
		{"none fills an optional", "fn f() -> Text? {\n  return none\n}", true},
		{"optional compared with none", "fn f(a: Money?) -> Bool {\n  return a != none and none != a\n}", true},
		{"none never equals a plain value", "fn f(a: Int) -> Bool {\n  return a == none\n}", false},
		{"optional list compared with none", "fn f(a: [Int]?) -> Int {\n  if a != none { return len(a) }\n  return 0\n}", true},
		{"lists are not compared", "fn f(a: [Int], b: [Int]) -> Bool {\n  return a == b\n}", false},
		{"time compares", "fn f(a: Time) -> Bool {\n  return a < now()\n}", true},
		{"list of floats", "fn f() -> [Float] {\n  return [1, 2.5]\n}", true},
		{"empty list", "fn f() -> [Text] {\n  return []\n}", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, diags := parser.ParseFile(token.NewFile("main.vibe", []byte("app A {}\n\n"+tt.body+"\n")))
			if len(diags) > 0 {
				t.Fatalf("syntax: %v", diags)
			}
			_, diags = Check([]*ast.File{file})
			if ok := len(diags) == 0; ok != tt.ok {
				t.Errorf("ok = %v, want %v: %v", ok, tt.ok, diags)
			}
		})
	}
}

func TestExpectText(t *testing.T) {
	prog := check(t, "app A {}\n\nfeature F {\n  test \"t\" {\n    let n = 2\n    expect (n + 1)  ==   3 # spaced\n    expect not (n > 5)\n  }\n}\n")
	var got []string
	for _, s := range prog.Features[0].Tests[0].Body.Stmts {
		if e, ok := s.(*ir.Expect); ok {
			got = append(got, e.Text)
		}
	}
	want := []string{"(n + 1)  ==   3", "not (n > 5)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("expect texts = %q, want %q", got, want)
	}
}
