package codf // import "go.spiff.io/codf"

import (
	"strings"
	"testing"
	"time"
)

func parse(in string) (*Document, error) {
	r := strings.NewReader(in)
	l := NewLexer(r)
	p := NewParser()
	if err := p.Parse(l); err != nil {
		return nil, err
	}
	return p.Document(), nil
}

func mustParse(t *testing.T, in string) *Document {
	doc, err := parse(in)
	if err != nil {
		t.Fatalf("Parse(..) error = %v; want nil", err)
	}
	t.Logf("-------- DOCUMENT --------\n%s\n------ END DOCUMENT ------", doc)
	return doc
}

func mustNotParse(t *testing.T, in string) *Document {
	doc, err := parse(in)
	if err == nil {
		t.Fatalf("Parse(..) error = %v; want error", err)
	}
	t.Logf("Parse(..) error = %v", err)
	return doc
}

// parseTestCase is used to describe and run a parser test, optionally as a subtest.
type parseTestCase struct {
	Name string
	Src  string
	Doc  *Document
	Fun  func(*testing.T, string) *Document
}

func (p parseTestCase) RunSubtest(t *testing.T) {
	t.Run(p.Name, p.Run)

}

func (p parseTestCase) Run(t *testing.T) {
	fn := p.Fun
	if fn == nil {
		fn = mustParse
	}
	doc := fn(t, p.Src)
	objectsEqual(t, "", doc, p.Doc)
}

func TestParseAST(t *testing.T) {
	cases := []parseTestCase{
		{
			Name: "Empty",
			Src:  "",
			Doc:  doc(),
		},
		{
			Name: "EmptyArrayMap",
			Src:  `foo 1 { bar [] #{}; } map #{} [];' eof`,
			Doc: doc().section("foo", 1).
				statement("bar", []ExprNode{}, mkmap()).
				up().statement("map", mkmap(), []ExprNode{}).
				Doc(),
		},
		{
			Name: "NestedArrayMaps",
			Src: `foo [' Nested map inside array
					#{ k [  1       ' integer
						"2"     ' string
						three   ' bareword (string)
						true    ' bool (bareword->bool)
						` + "`true`" + ` ' raw quote bool
						]
					   ` + "`raw`" + ` bare
					}
				];`,
			Doc: doc().statement("foo", mkexpr([]ExprNode{
				mkmap(
					"k", mkexpr(mkexprs(1, "2", "three", true, "true")),
					"raw", "bare",
				),
			})).Doc(),
		},
		{
			Name: "MinimalSpace",
			Src:  `sect[]#{}{stmt #{k[2]"p"#{}}true[false];}`,
			Doc: doc().section("sect", mkexprs(), mkmap()).
				statement("stmt", mkmap("k", mkexprs(2), "p", mkmap()), true, mkexprs(false)).
				Doc(),
		},
		{
			Name: "AllLiterals",
			Src: `
				stmt
					yes no
					true false
					1234 -1.234
					0600 0b101
					16#ffff 0xFFFF
					"\u1234` + "\n" + `\x00"
					"foo" bar
					0.5h30s0.5s500us0.5us1ns
					#/foobar/ #//
					0/1 120/4
				{
					inside Yes YES yes yeS ' Last is always a bareword here
						No NO no nO
						True TRUE true truE
						False FALSE false falsE;
				}
			`,
			Doc: doc().section("stmt",
				true, false,
				true, false,
				1234, mkdec("-1.234"),
				0600, 5,
				0xffff, 0xffff,
				"\u1234\n\x00",
				"foo", "bar",
				time.Hour/2+time.Minute/2+time.Second/2+time.Millisecond/2+time.Microsecond/2+time.Nanosecond,
				mkregex("foobar"), mkregex(""),
				mkrat(0, 1), mkrat(30, 1),
			).statement("inside",
				true, true, true, "yeS",
				false, false, false, "nO",
				true, true, true, "truE",
				false, false, false, "falsE",
			).Doc(),
		},
		{Fun: mustNotParse, Name: "BadMapClose", Src: `src #{;};`},
		{Fun: mustNotParse, Name: "BadMapClose", Src: `src #{ k };`},
		{Fun: mustNotParse, Name: "BadMapClose", Src: `src #{ 1234 five };`},
		{Fun: mustNotParse, Name: "BadMapClose", Src: `src #{ k ];`},
		{Fun: mustNotParse, Name: "BadMapClose", Src: `src #{];`},
		{Fun: mustNotParse, Name: "BadArrayClose", Src: `src [;];`},
		{Fun: mustNotParse, Name: "BadArrayClose", Src: `src [};`},
		{Fun: mustNotParse, Name: "BadStatementClose", Src: `src };`},
		{Fun: mustNotParse, Name: "BadStatementClose", Src: `src ];`},
		{Fun: mustNotParse, Name: "BadStatementClose", Src: `src`},
		{Fun: mustNotParse, Name: "BadSectionClose", Src: `src {`},
		{Fun: mustNotParse, Name: "BadSectionClose", Src: `src {]`},
		{Fun: mustNotParse, Name: "BadSectionClose", Src: `src { ' comment`},
		{Fun: mustNotParse, Name: "BadSectionClose", Src: `}`},
		{Fun: mustNotParse, Name: "BadSectionClose", Src: `]`},
	}

	for _, c := range cases {
		c.RunSubtest(t)
	}
}

func TestParseExample(t *testing.T) {
	const exampleSource = `server go.spiff.io {
    listen 0.0.0.0:80;
    control unix:///var/run/httpd.sock;
    proxy unix:///var/run/go-redirect.sock {
        strip-x-headers yes;
        log-access no;
    }
    ' keep caches in 64mb of memory
    cache memory 64mb {
         expire 10m 404;
         expire 1h  301 302;
         expire 5m  200;
    }
}`

	parseTestCase{
		Src: exampleSource,
		Doc: doc().
			section("server", "go.spiff.io").
			/* server */ statement("listen", "0.0.0.0:80").
			/* server */ statement("control", "unix:///var/run/httpd.sock").
			/* server */ section("proxy", "unix:///var/run/go-redirect.sock").
			/* server */ /* proxy */ statement("strip-x-headers", true).
			/* server */ /* proxy */ statement("log-access", false).
			/* server */ up().
			/* server */ section("cache", "memory", "64mb").
			/* server */ /* cache */ statement("expire", time.Minute*10, 404).
			/* server */ /* cache */ statement("expire", time.Hour, 301, 302).
			/* server */ /* cache */ statement("expire", time.Minute*5, 200).
			Doc(),
	}.Run(t)
}

func TestParseEmpty(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		objectsEqual(t, "",
			mustParse(t, ""),
			new(Document),
		)
	})
	t.Run("Whitespace", func(t *testing.T) {
		objectsEqual(t, "",
			mustParse(t, " "),
			new(Document),
		)
	})
	t.Run("Whitespaces", func(t *testing.T) {
		objectsEqual(t, "",
			mustParse(t, "\t \t\n\r\n\r\n \n "),
			new(Document),
		)
	})
	t.Run("Semicolon", func(t *testing.T) {
		objectsEqual(t, "",
			mustParse(t, ";"),
			new(Document),
		)
	})
	t.Run("Semicolons", func(t *testing.T) {
		objectsEqual(t, "",
			mustParse(t, ";;;;;;;"),
			new(Document),
		)
	})
	t.Run("Mixed", func(t *testing.T) {
		objectsEqual(t, "",
			mustParse(t, "   ;\n\t; ;\n;"),
			new(Document),
		)
	})
}
