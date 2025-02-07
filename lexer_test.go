package codf // import "go.spiff.io/codf"

import (
	"bytes"
	"fmt"
	"io"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/regexp"
)

func TestInvalidTokenName(t *testing.T) {
	const want = "invalid"
	const tok32 = TokenKind(0xffffffff)
	defer setlogf(t)()
	if got := tok32.String(); got != want {
		t.Errorf("Token(%08x) = %q; want %q", tok32, got, want)
	}
}

func regexCase(rx string) tokenCase {
	return tokenCase{
		Token: Token{
			Kind:  TRegexp,
			Raw:   []byte("#/" + strings.ReplaceAll(rx, "/", "\\/") + "/"),
			Value: regexp.MustCompile(rx),
		},
	}
}

func durCase(text string) tokenCase {
	d, err := time.ParseDuration(text)
	if err != nil {
		panic("error creating duration: " + err.Error())
	}
	return tokenCase{
		Token: Token{
			Kind:  TDuration,
			Raw:   []byte(text),
			Value: d,
		},
	}
}

func floatCase(text string, precision uint) tokenCase {
	if precision <= 0 {
		precision = DefaultPrecision
	}
	var f big.Float
	f.SetPrec(precision)
	if _, ok := f.SetString(text); !ok {
		panic("error creating float " + text)
	}
	return tokenCase{
		Token: Token{
			Kind:  TFloat,
			Raw:   []byte(text),
			Value: &f,
		},
	}
}

func baseCase(val int64, base int) tokenCase {
	t := val
	neg := ""
	if t < 0 {
		neg = "-"
		t = -t
	}
	formatted := fmt.Sprintf("%s%d#%s", neg, base, strconv.FormatInt(t, base))
	return intCase(val, formatted)
}

func intCase(val int64, raw string) tokenCase {
	kind := TInteger
	switch s := strings.ToLower(strings.TrimLeft(raw, "-+")); {
	case strings.HasPrefix(s, "0x"):
		kind = THex
	case strings.HasPrefix(s, "0b"):
		kind = TBinary
	case len(s) > 1 && strings.HasPrefix(s, "0"):
		kind = TOctal
	case strings.Contains(s, "#"):
		kind = TBaseInt
	}
	return tokenCase{
		Token: Token{
			Kind:  kind,
			Raw:   []byte(raw),
			Value: big.NewInt(val),
		},
	}
}

func wordCase(word string) tokenCase {
	return tokenCase{
		Token: Token{
			Kind:  TWord,
			Raw:   []byte(word),
			Value: word,
		},
	}
}

func commentCase(text string) tokenCase {
	return tokenCase{
		Token: Token{
			Kind:  TComment,
			Raw:   []byte("//" + text),
			Value: text,
		},
	}
}

func quoteCase(str string) tokenCase {
	return tokenCase{
		Token: Token{
			Kind:  TString,
			Raw:   []byte(`"` + str + `"`),
			Value: str,
		},
	}
}

func compareValue(l, r any) bool {
	switch ll := l.(type) {
	case nil:
		// Don't compare -- nil isn't a value and only used for punctuation.
		return true
	case bool:
		rr, ok := r.(bool)
		return ok && ll == rr
	case string:
		rr, ok := r.(string)
		return ok && ll == rr
	case time.Duration:
		rr, ok := r.(time.Duration)
		return ok && ll == rr
	case *big.Int:
		rr, ok := r.(*big.Int)
		return ok && ll.Cmp(rr) == 0
	case *big.Float:
		rr, ok := r.(*big.Float)
		return ok && ll.Cmp(rr) == 0
	case *big.Rat:
		rr, ok := r.(*big.Rat)
		return ok && ll.Cmp(rr) == 0
	case *regexp.Regexp:
		rr, ok := r.(*regexp.Regexp)
		return ok && ll.String() == rr.String()
	default:
		// Not a known type -- can't be valid
		return false
	}
}

func reader(s string) *bytes.Buffer {
	return bytes.NewBuffer([]byte(s))
}

func requireEOF(t *testing.T, b *bytes.Buffer) {
	if b.Len() > 0 {
		t.Fatalf("expected EOF; %d bytes remaining", b.Len())
	}
}

func checkToken(t *testing.T, prefix string, got, want Token) {
	if got.Kind != want.Kind {
		t.Errorf("%stok.Kind = %v; want %v", prefix, got.Kind, want.Kind)
	}
	if want.Start.Column > 0 && got.Start != want.Start {
		t.Errorf("%stok.Start = %#v; want %#v", prefix, got.Start, want.Start)
	}
	if want.End.Column > 0 && got.End != want.End {
		t.Errorf("%stok.End = %#v; want %#v", prefix, got.End, want.End)
	}
	if want.Raw == nil {
		// Skip check
	} else if want.Raw != nil && got.Raw == nil {
		t.Errorf("%stok.Raw = nil; want %q", prefix, want.Raw)
	} else if !bytes.Equal(got.Raw, want.Raw) {
		t.Errorf("%stok.Raw = %q; want %q", prefix, got.Raw, want.Raw)
	}

	if !compareValue(want.Value, got.Value) {
		t.Errorf("%stok.Value = %T(%#v); want %T(%#v)", prefix,
			got.Value, got.Value,
			want.Value, want.Value)
	}

	if t.Failed() {
		t.Logf("%stok.Raw = %q", prefix, got.Raw)
	}
}

type tokenCase struct {
	Token
	Err bool
}

// Common punctuation tokens
var (
	_error        = tokenCase{Err: true}
	_ws           = tokenCase{Token: Token{Kind: TWhitespace}}
	_eof          = tokenCase{Token: Token{Kind: TEOF}}
	_semicolon    = tokenCase{Token: Token{Kind: TSemicolon}}
	_curlopen     = tokenCase{Token: Token{Kind: TCurlOpen}}
	_curlclose    = tokenCase{Token: Token{Kind: TCurlClose}}
	_bracketopen  = tokenCase{Token: Token{Kind: TBracketOpen}}
	_bracketclose = tokenCase{Token: Token{Kind: TBracketClose}}
	_mapopen      = tokenCase{Token: Token{Kind: TMapOpen}}
	_comment      = tokenCase{Token: Token{Kind: TComment}}
)

type tokenSeq []tokenCase

func (seq tokenSeq) RunFlags(t *testing.T, flags LexerFlag, input string) {
	buf := reader(input)
	lex := NewLexer(buf)
	lex.Flags = flags
	lex.Name = "test.codf"
	if seq.RunWithLexer(t, lex) {
		requireEOF(t, buf)
	}
}

func (seq tokenSeq) Run(t *testing.T, input string) {
	seq.RunFlags(t, LexDefaultFlags, input)
}

func (seq tokenSeq) RunWithLexer(t *testing.T, lex *Lexer) bool {
	for i, want := range seq {
		prefix := fmt.Sprintf("%d: ", i+1)

		tok, err := lex.ReadToken()
		if want.Err && err == nil {
			t.Errorf("%sgot error = nil; want error", prefix)
		} else if !want.Err && err != nil {
			t.Errorf("%sgot error = %v; want %v", prefix, err, want.Kind)
		}

		if want.Err && err != nil {
			return false
		}

		checkToken(t, prefix, tok, want.Token)

		if t.Failed() {
			return false
		}
	}
	return true
}

type tokenSeqTest struct {
	Name  string
	Input string
	Seq   tokenSeq
}

func (tt *tokenSeqTest) Run(t *testing.T) {
	t.Run(tt.Name, func(t *testing.T) {
		defer setlogf(t)()
		tt.Seq.Run(t, tt.Input)
	})
}

func TestComment(t *testing.T) {
	defer setlogf(t)()
	tokenSeq{
		{Token: Token{Kind: TComment, Raw: []byte("//")}},
		{Token: Token{Kind: TWhitespace, Raw: []byte("\n")}},
		{Token: Token{Kind: TComment, Raw: []byte("// foo bar baz")}},
		_eof,
	}.Run(t, "//\n// foo bar baz")
}

func TestBareword(t *testing.T) {
	defer setlogf(t)()
	tokenSeq{
		{Token: Token{
			Kind:  TWhitespace,
			Start: Location{Name: "test.codf", Offset: 0, Line: 1, Column: 1},
			End:   Location{Name: "test.codf", Offset: 1, Line: 1, Column: 2},
		}},
		{Token: Token{
			Kind:  TWord,
			Raw:   []byte(".foo$bar#baz=quux"),
			Start: Location{Name: "test.codf", Offset: 1, Line: 1, Column: 2},
			End:   Location{Name: "test.codf", Offset: 18, Line: 1, Column: 19},
			Value: ".foo$bar#baz=quux",
		}},
		_ws,
		{Token: Token{
			Kind:  TWord,
			Raw:   []byte("10.0.0.0/8"),
			Start: Location{Name: "test.codf", Offset: 20, Line: 2, Column: 2},
			End:   Location{Name: "test.codf", Offset: 30, Line: 2, Column: 12},
			Value: "10.0.0.0/8",
		}},
		_ws,
		{Token: Token{
			Kind:  TWord,
			Raw:   []byte("#"),
			Start: Location{Name: "test.codf", Offset: 31, Line: 2, Column: 13},
			End:   Location{Name: "test.codf", Offset: 32, Line: 2, Column: 14},
			Value: "#",
		}},
		_ws,
		{Token: Token{
			Kind:  TWord,
			Raw:   []byte("#f"),
			Start: Location{Name: "test.codf", Offset: 33, Line: 2, Column: 15},
			End:   Location{Name: "test.codf", Offset: 35, Line: 2, Column: 17},
			Value: "#f",
		}},
		_ws,
		{Token: Token{
			Kind:  TWord,
			Raw:   []byte("+"),
			Start: Location{Name: "test.codf", Offset: 36, Line: 2, Column: 18},
			End:   Location{Name: "test.codf", Offset: 37, Line: 2, Column: 19},
			Value: "+",
		}},
		_ws,
		{Token: Token{
			Kind:  TWord,
			Raw:   []byte("-"),
			Start: Location{Name: "test.codf", Offset: 38, Line: 2, Column: 20},
			End:   Location{Name: "test.codf", Offset: 39, Line: 2, Column: 21},
			Value: "-",
		}},
		_semicolon,
		_ws,
		{Token: Token{Kind: TComment, Raw: []byte("// foo"), Value: " foo"}},
		_ws, wordCase("+f"),
		_ws, wordCase("1/1f"),
		_ws, wordCase("1nq"),
		_ws, wordCase("1s1.s"),
		_ws, wordCase("#foo"),
		_ws, wordCase("/foo/"),
		_ws, wordCase("#"),
		quoteCase("foo"),
		_eof,
	}.Run(t, "\t.foo$bar#baz=quux\n"+
		"\t10.0.0.0/8 # #f + -; // foo\n"+
		"\n"+
		"+f 1/1f 1nq 1s1.s #foo /foo/ #\"foo\"",
	)
}

func TestWhitespace(t *testing.T) {
	defer setlogf(t)()
	tokenSeq{
		{
			Token: Token{
				Start: Location{Name: "test.codf", Column: 1, Line: 1, Offset: 0},
				End:   Location{Name: "test.codf", Column: 3, Line: 3, Offset: 6},
				Kind:  TWhitespace,
			},
		},
	}.Run(t, " \n\r\n\t ")
}

type flagTest struct {
	Flags LexerFlag
	Seq   string
	On    tokenSeq
	Off   tokenSeq
}

func (f flagTest) Test(t *testing.T) {
	t.Run("On", func(t *testing.T) {
		defer setlogf(t)()
		f.On.RunFlags(t, f.Flags, f.Seq)
	})
	t.Run("Off", func(t *testing.T) {
		defer setlogf(t)()
		f.Off.RunFlags(t, LexDefaultFlags, f.Seq)
	})
}

func (f flagTest) WithFlags(flag LexerFlag) flagTest {
	f.Flags = flag
	return f
}

func TestLexNoRegexpsFlag(t *testing.T) {
	t.Run("Regexp", flagTest{
		Flags: LexNoRegexps,
		Seq:   `#/r x/`,
		On:    tokenSeq{wordCase("#/r"), _ws, wordCase("x/"), _eof},
		Off:   tokenSeq{regexCase("r x"), _eof},
	}.Test)
}

func TestLexNoBooleansFlag(t *testing.T) {
	flagTest{
		Flags: LexNoBools,
		Seq:   `TRUE true Yes FALSE`,
		On: tokenSeq{
			{Token: Token{Kind: TWord, Raw: []byte("TRUE"), Value: "TRUE"}},
			_ws, {Token: Token{Kind: TWord, Raw: []byte("true"), Value: "true"}},
			_ws, {Token: Token{Kind: TWord, Raw: []byte("Yes"), Value: "Yes"}},
			_ws, {Token: Token{Kind: TWord, Raw: []byte("FALSE"), Value: "FALSE"}},
			_eof,
		},
		Off: tokenSeq{
			{Token: Token{Kind: TBoolean, Raw: []byte("TRUE"), Value: true}},
			_ws, {Token: Token{Kind: TBoolean, Raw: []byte("true"), Value: true}},
			_ws, {Token: Token{Kind: TBoolean, Raw: []byte("Yes"), Value: true}},
			_ws, {Token: Token{Kind: TBoolean, Raw: []byte("FALSE"), Value: false}},
			_eof,
		},
	}.Test(t)
}

func TestLexNoDurationsFlag(t *testing.T) {
	durations := flagTest{
		Flags: LexNoDurations,
		Seq: `
			-0s -1ns -0ns -0.0s
			-1h234m7s -1h -60m
			-0.05s -500ms
			-0.5ms -500us -500μs
			+0s +1ns +0ns +0.0s
			+1h234m7s +1h +60m
			+0.05s +500ms
			+0.5ms +500us +500μs
			0s 1ns 0ns 0.0s
			1h234m7s 1h 60m
			0.05s 500ms
			0.5ms 500us 500μs
			1h0.25m
			1h0m0.05s
		`,
		On: tokenSeq{
			// Negative sign
			_ws, wordCase("-0s"),
			_ws, wordCase("-1ns"),
			_ws, wordCase("-0ns"),
			_ws, wordCase("-0.0s"),
			_ws, wordCase("-1h234m7s"),
			_ws, wordCase("-1h"),
			_ws, wordCase("-60m"),
			_ws, wordCase("-0.05s"),
			_ws, wordCase("-500ms"),
			_ws, wordCase("-0.5ms"),
			_ws, wordCase("-500us"),
			_ws, wordCase("-500μs"),
			// Positive sign
			_ws, wordCase("+0s"),
			_ws, wordCase("+1ns"),
			_ws, wordCase("+0ns"),
			_ws, wordCase("+0.0s"),
			_ws, wordCase("+1h234m7s"),
			_ws, wordCase("+1h"),
			_ws, wordCase("+60m"),
			_ws, wordCase("+0.05s"),
			_ws, wordCase("+500ms"),
			_ws, wordCase("+0.5ms"),
			_ws, wordCase("+500us"),
			_ws, wordCase("+500μs"),
			// No sign
			_ws, wordCase("0s"),
			_ws, wordCase("1ns"),
			_ws, wordCase("0ns"),
			_ws, wordCase("0.0s"),
			_ws, wordCase("1h234m7s"),
			_ws, wordCase("1h"),
			_ws, wordCase("60m"),
			_ws, wordCase("0.05s"),
			_ws, wordCase("500ms"),
			_ws, wordCase("0.5ms"),
			_ws, wordCase("500us"),
			_ws, wordCase("500μs"),
			_ws, wordCase("1h0.25m"),
			_ws, wordCase("1h0m0.05s"),
			_ws, _eof,
		},
		Off: tokenSeq{
			// Negative sign
			_ws, durCase("-0s"),
			_ws, durCase("-1ns"),
			_ws, durCase("-0ns"),
			_ws, durCase("-0.0s"),
			_ws, durCase("-1h234m7s"),
			_ws, durCase("-1h"),
			_ws, durCase("-60m"),
			_ws, durCase("-0.05s"),
			_ws, durCase("-500ms"),
			_ws, durCase("-0.5ms"),
			_ws, durCase("-500us"),
			_ws, durCase("-500μs"),
			// Positive sign
			_ws, durCase("+0s"),
			_ws, durCase("+1ns"),
			_ws, durCase("+0ns"),
			_ws, durCase("+0.0s"),
			_ws, durCase("+1h234m7s"),
			_ws, durCase("+1h"),
			_ws, durCase("+60m"),
			_ws, durCase("+0.05s"),
			_ws, durCase("+500ms"),
			_ws, durCase("+0.5ms"),
			_ws, durCase("+500us"),
			_ws, durCase("+500μs"),
			// No sign
			_ws, durCase("0s"),
			_ws, durCase("1ns"),
			_ws, durCase("0ns"),
			_ws, durCase("0.0s"),
			_ws, durCase("1h234m7s"),
			_ws, durCase("1h"),
			_ws, durCase("60m"),
			_ws, durCase("0.05s"),
			_ws, durCase("500ms"),
			_ws, durCase("0.5ms"),
			_ws, durCase("500us"),
			_ws, durCase("500μs"),
			_ws, durCase("1h0.25m"),
			_ws, durCase("1h0m0.05s"),
			_ws, _eof,
		},
	}

	durations.Test(t)
	t.Run("NoNumbers-Durations", durations.WithFlags(LexNoNumbers).Test)
}

func TestLexNoRationalsFlag(t *testing.T) {
	var (
		rZero = big.NewRat(0, 1)
		rNeg  = big.NewRat(-3, 4)
		rPos  = big.NewRat(3, 4)
	)

	rationals := flagTest{
		Flags: LexNoRationals,
		Seq: `
			 0/100 -0/200 -0/1 0/1
			-3/4 -6/8 -75/100
			+3/4 +6/8 +75/100
			 3/4 6/8 75/100
		`,
		On: tokenSeq{
			// Zero
			_ws, wordCase("0/100"),
			_ws, wordCase("-0/200"),
			_ws, wordCase("-0/1"),
			_ws, wordCase("0/1"),
			// Negative sign
			_ws, wordCase("-3/4"),
			_ws, wordCase("-6/8"),
			_ws, wordCase("-75/100"),
			// Positive sign
			_ws, wordCase("+3/4"),
			_ws, wordCase("+6/8"),
			_ws, wordCase("+75/100"),
			// No sign
			_ws, wordCase("3/4"),
			_ws, wordCase("6/8"),
			_ws, wordCase("75/100"),
			_ws, _eof,
		},
		Off: tokenSeq{
			// Zero
			_ws, {Token: Token{Kind: TRational, Value: rZero, Raw: []byte("0/100")}},
			_ws, {Token: Token{Kind: TRational, Value: rZero, Raw: []byte("-0/200")}},
			_ws, {Token: Token{Kind: TRational, Value: rZero, Raw: []byte("-0/1")}},
			_ws, {Token: Token{Kind: TRational, Value: rZero, Raw: []byte("0/1")}},
			// Negative sign
			_ws, {Token: Token{Kind: TRational, Value: rNeg, Raw: []byte("-3/4")}},
			_ws, {Token: Token{Kind: TRational, Value: rNeg, Raw: []byte("-6/8")}},
			_ws, {Token: Token{Kind: TRational, Value: rNeg, Raw: []byte("-75/100")}},
			// Positive sign
			_ws, {Token: Token{Kind: TRational, Value: rPos, Raw: []byte("+3/4")}},
			_ws, {Token: Token{Kind: TRational, Value: rPos, Raw: []byte("+6/8")}},
			_ws, {Token: Token{Kind: TRational, Value: rPos, Raw: []byte("+75/100")}},
			// No sign
			_ws, {Token: Token{Kind: TRational, Value: rPos, Raw: []byte("3/4")}},
			_ws, {Token: Token{Kind: TRational, Value: rPos, Raw: []byte("6/8")}},
			_ws, {Token: Token{Kind: TRational, Value: rPos, Raw: []byte("75/100")}},
			_ws, _eof,
		},
	}

	rationals.Test(t)
	t.Run("NoNumbers-Rationals", rationals.WithFlags(LexNoNumbers).Test)
}

func TestLexNoFloatsFlag(t *testing.T) {
	floats := flagTest{
		Flags: LexNoFloats,
		Seq: `
			-0.0 -0.5 -0.0e0 -0.0E0
			-1.2345 -12345e-4 -1.2345e4 -1.2345e+4
			+0.0 +0.5 +0.0e0 +0.0E0
			+1.2345 +12345E-4 +1.2345E4 +1.2345E+4
			[0.0] #{k 0.5} 0e123 0.0e0 0.0E0
			1.2345 12345e-4 1.2345e4 1.2345e+4
		`,
		On: tokenSeq{
			// Negative sign
			_ws, wordCase("-0.0"),
			_ws, wordCase("-0.5"),
			_ws, wordCase("-0.0e0"),
			_ws, wordCase("-0.0E0"),
			_ws, wordCase("-1.2345"),
			_ws, wordCase("-12345e-4"),
			_ws, wordCase("-1.2345e4"),
			_ws, wordCase("-1.2345e+4"),
			// Positive sign
			_ws, wordCase("+0.0"),
			_ws, wordCase("+0.5"),
			_ws, wordCase("+0.0e0"),
			_ws, wordCase("+0.0E0"),
			_ws, wordCase("+1.2345"),
			_ws, wordCase("+12345E-4"),
			_ws, wordCase("+1.2345E4"),
			_ws, wordCase("+1.2345E+4"),
			// No sign
			_ws, _bracketopen, wordCase("0.0"), _bracketclose,
			_ws, _mapopen,
			{Token: Token{Kind: TWord, Value: "k"}},
			_ws, wordCase("0.5"),
			_curlclose,
			_ws, wordCase("0e123"),
			_ws, wordCase("0.0e0"),
			_ws, wordCase("0.0E0"),
			_ws, wordCase("1.2345"),
			_ws, wordCase("12345e-4"),
			_ws, wordCase("1.2345e4"),
			_ws, wordCase("1.2345e+4"),
			_ws, _eof,
		},
		Off: tokenSeq{
			// Negative sign
			_ws, floatCase("-0.0", 0),
			_ws, floatCase("-0.5", 0),
			_ws, floatCase("-0.0e0", 0),
			_ws, floatCase("-0.0E0", 0),
			_ws, floatCase("-1.2345", 0),
			_ws, floatCase("-12345e-4", 0),
			_ws, floatCase("-1.2345e4", 0),
			_ws, floatCase("-1.2345e+4", 0),
			// Positive sign
			_ws, floatCase("+0.0", 0),
			_ws, floatCase("+0.5", 0),
			_ws, floatCase("+0.0e0", 0),
			_ws, floatCase("+0.0E0", 0),
			_ws, floatCase("+1.2345", 0),
			_ws, floatCase("+12345E-4", 0),
			_ws, floatCase("+1.2345E4", 0),
			_ws, floatCase("+1.2345E+4", 0),
			// No sign
			_ws, _bracketopen, floatCase("0.0", 0), _bracketclose,
			_ws, _mapopen,
			{Token: Token{Kind: TWord, Value: "k"}},
			_ws, floatCase("0.5", 0),
			_curlclose,
			_ws, floatCase("0e123", 0),
			_ws, floatCase("0.0e0", 0),
			_ws, floatCase("0.0E0", 0),
			_ws, floatCase("1.2345", 0),
			_ws, floatCase("12345e-4", 0),
			_ws, floatCase("1.2345e4", 0),
			_ws, floatCase("1.2345e+4", 0),
			_ws, _eof,
		},
	}

	floats.Test(t)
	t.Run("NoNumbers-Floats", floats.WithFlags(LexNoNumbers).Test)
}

func TestLexNoBaseIntsFlag(t *testing.T) {
	baseInts := flagTest{
		Seq: `
			0xff 0XFF
			0b1111 0B1111
			0600 0000 -0600
			3#210 -3#210
		`,
		Flags: LexNoBaseInts,
		On: tokenSeq{
			_ws, wordCase("0xff"),
			_ws, wordCase("0XFF"),
			_ws, wordCase("0b1111"),
			_ws, wordCase("0B1111"),
			_ws, wordCase("0600"),
			_ws, wordCase("0000"),
			_ws, wordCase("-0600"),
			_ws, wordCase("3#210"),
			_ws, wordCase("-3#210"),
			_ws, _eof,
		},
		Off: tokenSeq{
			_ws, intCase(255, "0xff"),
			_ws, intCase(255, "0XFF"),
			_ws, intCase(15, "0b1111"),
			_ws, intCase(15, "0B1111"),
			_ws, intCase(384, "0600"),
			_ws, intCase(0, "0000"),
			_ws, intCase(-384, "-0600"),
			_ws, intCase(21, "3#210"),
			_ws, intCase(-21, "-3#210"),
			_ws, _eof,
		},
	}

	baseInts.Test(t)
	t.Run("NoNumbers-BaseInt", baseInts.WithFlags(LexNoNumbers).Test)
}

func TestStatement(t *testing.T) {
	defer setlogf(t)()
	tokenSeq{
		_ws, {Token: Token{Kind: TWord, Raw: []byte("stmt"), Value: "stmt"}},
		_ws, {Token: Token{Kind: TInteger, Raw: []byte("-1234"), Value: big.NewInt(-1234)}},
		_ws, {Token: Token{Kind: TOctal, Raw: []byte("+0600"), Value: big.NewInt(0600)}},
		_ws, {Token: Token{Kind: THex, Raw: []byte("-0xf"), Value: big.NewInt(-15)}},
		_ws, {Token: Token{Kind: THex, Raw: []byte("0x12f"), Value: big.NewInt(303)}},
		_ws, wordCase("${FOO:-${BAZ:-Default}}"),
		_semicolon,
		_ws, wordCase("stmt/2"), _semicolon,
		_ws, wordCase("stmt{}"), _semicolon,
		_ws, wordCase("invalid"), _curlclose,
		_ws, wordCase("sect"), _ws, _curlopen, _curlclose,
		_ws, wordCase("a"), _semicolon,
		_ws, wordCase("b{}"),
		_ws, wordCase("c//foo"),
		_ws, commentCase("foo"),
		_ws, commentCase(" foo bar baz"),
		_ws, wordCase("#[foo]"),
		_ws, wordCase("$[foo]"),
		_ws, wordCase("${foo}"),
		_ws, wordCase("${{foo}}"),
		_ws, wordCase("${[foo}]"),
		_ws, wordCase("${foo}"), _curlclose,
		_ws, wordCase("${foo]"), _bracketclose,
		_ws, _semicolon, _semicolon,
		_ws, _eof,
	}.Run(t, `
		stmt -1234 +0600 -0xf 0x12f ${FOO:-${BAZ:-Default}};
		stmt/2;
		stmt{};
		invalid}
		sect {}
		a;
		b{}
		c//foo //foo
		// foo bar baz
		#[foo] $[foo] ${foo} ${{foo}} ${[foo}] ${foo}} ${foo]]
		;;
		`)
}

func TestInvalidUTF8(t *testing.T) {
	defer setlogf(t)()
	tokenSeq{_error}.Run(t, "\xff")
}

func TestStatementInvalid(t *testing.T) {
	defer setlogf(t)()
	tokenSeq{
		{Token: Token{Kind: TWord, Raw: []byte("a"), Value: "a"}},
		_eof,
	}.Run(t, `a`)
	tokenSeq{
		{Token: Token{Kind: TWord, Raw: []byte("a"), Value: "a"}},
		_error,
	}.Run(t, "a\x00")
}

func TestRegexp(t *testing.T) {
	defer setlogf(t)()
	regex := regexp.MustCompile
	tokenSeq{
		{Token: Token{Kind: TWord, Raw: []byte("stmt"), Value: "stmt"}},
		_ws, {Token: Token{Kind: TRegexp, Raw: []byte("#/foo\\/bar\n/"), Value: regex("foo/bar\n")}},
		_ws, {Token: Token{Kind: TRegexp, Raw: []byte("#//"), Value: regex("")}},
		_ws, {Token: Token{Kind: TRegexp, Raw: []byte("#/\\./"), Value: regex("\\.")}},
		_semicolon,
		_eof,
	}.Run(t, "stmt #/foo\\/bar\n/ #// #/\\./;")

	// Test that regexp lexing is disabled
	tokenSeq{
		wordCase("#//"), _ws,
		wordCase("#/foo"), _ws,
		wordCase("bar/"),
	}.RunFlags(t, LexNoRegexps, "#// #/foo bar/")

	// Fail on EOF at points in regexp parsing
	// EOF in regexp (start)
	tokenSeq{
		{Token: Token{Kind: TWord, Raw: []byte("stmt"), Value: "stmt"}},
		_ws, _error,
	}.Run(t, "stmt #/")

	// EOF in regexp (middle)
	tokenSeq{
		{Token: Token{Kind: TWord, Raw: []byte("stmt"), Value: "stmt"}},
		_ws, _error,
	}.Run(t, "stmt #/foobar")
}

func TestString(t *testing.T) {
	defer setlogf(t)()
	tokenSeq{
		{Token: Token{Kind: TWord, Raw: []byte("stmt"), Value: "stmt"}},
		_ws, {Token: Token{Kind: TString, Raw: []byte(`""`), Value: ""}},
		_ws, {Token: Token{Kind: TString, Raw: []byte(`"simple string"`), Value: "simple string"}},
		_ws, {Token: Token{Kind: TString, Raw: []byte(`"\a\b\f\n\r\t\v\\\""`), Value: "\a\b\f\n\r\t\v\\\""}},
		_ws, {Token: Token{Kind: TString, Raw: []byte(`"\123\xff\u7fff\U00001234"`), Value: "\123\xff\u7fff\U00001234"}},
		_ws, {Token: Token{Kind: TString, Raw: []byte(`"\xFF"`), Value: "\xff"}},
		_ws,
		{Token: Token{Kind: TWord, Raw: []byte(`a`), Value: "a"}},
		{Token: Token{Kind: TString, Raw: []byte(`"b"`), Value: "b"}},
		{Token: Token{Kind: TWord, Raw: []byte(`c`), Value: "c"}},
		_ws, {Token: Token{Kind: TRawString, Raw: []byte("`\"foo\x00bar\"`"), Value: "\"foo\x00bar\""}},
		_ws, {Token: Token{Kind: TRawString, Raw: []byte("```foo\x00bar```"), Value: "`foo\x00bar`"}},
		_ws, {Token: Token{Kind: TRawString, Raw: []byte("`foo \\bar `` \\\\ baz\\```"), Value: "foo \\bar ` \\\\ baz\\`"}},
		_ws, {Token: Token{Kind: TRawString, Raw: []byte("`\\`"), Value: `\`}},
		_ws, {Token: Token{Kind: TRawString, Raw: []byte("````"), Value: "`"}},
		_ws, {Token: Token{Kind: TRawString, Raw: []byte("``"), Value: ""}},
		_ws, _semicolon,
		_eof,
	}.Run(t,
		`stmt   ""
			"simple string"
			"\a\b\f\n\r\t\v\\\""
			"\123\xff\u7fff\U00001234"
			"\xFF"
			a"b"c
			`+"`\"foo\x00bar\"`"+`
			`+"```foo\x00bar```"+`
			`+"`foo \\bar `` \\\\ baz\\```"+`
			`+"`\\`"+`
			`+"````"+`
			`+"``"+`
		;`)
}

func TestBaseInteger(t *testing.T) {
	defer setlogf(t)()
	num := big.NewInt(-12345)
	pos := big.NewInt(12345)
	for base := 2; base <= 36; base++ {
		basenum := pos.Text(base)
		lit := fmt.Sprintf("-%d#%s", base, basenum)
		stmt := "stmt " + lit + " foo 0;"
		tok := Token{
			Kind:  TBaseInt,
			Raw:   []byte(lit),
			Value: num,
		}

		(&tokenSeqTest{
			Name:  "Base-" + strconv.Itoa(base),
			Input: stmt,
			Seq: tokenSeq{
				{Token: Token{Kind: TWord, Value: "stmt"}},
				_ws, {Token: tok},
				_ws, {Token: Token{Kind: TWord, Value: "foo"}},
				_ws, {Token: Token{Kind: TInteger, Value: big.NewInt(0)}},
				_semicolon,
				_eof,
			},
		}).Run(t)
	}

	// Invalid things -- these become words
	badtext := []string{
		`0#0`,
		`1#0`,
		`37#zz`,
	}

	for _, c := range badtext {
		want := strings.TrimRight(c, ";")
		seq := tokenSeq{
			{Token: Token{Kind: TWord, Raw: []byte(want), Value: want}},
		}
		t.Run(c, func(t *testing.T) {
			defer setlogf(t)()
			seq.Run(t, c)
		})
	}
}

func TestInvalidNumber(t *testing.T) {
	tokenSeq{_error}.Run(t, "4#\x00")
	tokenSeq{_error}.Run(t, "1e0\x00")
}

func TestInvalidStrings(t *testing.T) {
	defer setlogf(t)()
	invalid := tokenSeq{
		{Token: Token{
			Kind:  TWord,
			Raw:   []byte("stmt"),
			Value: "stmt",
		}},
		_ws, _error,
	}

	cases := []tokenSeqTest{
		{Name: "Quote/EOF", Input: `stmt "`},
		{Name: "Quote/EOF", Input: `stmt "after`},
		{Name: "Quote/Octal-Invalid", Input: `stmt "\60z";`},
		{Name: "Quote/Octal-Invalid", Input: `stmt "\608";`},
		{Name: "Quote/Octal-EOF", Input: `stmt "\`},
		{Name: "Quote/Octal-EOF", Input: `stmt "\7`},
		{Name: "Quote/Octal-EOF", Input: `stmt "\60`},
		{Name: "Quote/Hex-Invalid", Input: `stmt "\xz";`},
		{Name: "Quote/Hex-Invalid", Input: `stmt "\xfz";`},
		{Name: "Quote/Hex-EOF", Input: `stmt "\x`},
		{Name: "Quote/Hex-EOF", Input: `stmt "\xf`},
		{Name: "Quote/Uni16-Invalid", Input: `stmt "\uff";`},
		{Name: "Quote/Uni16-Invalid", Input: `stmt "\uffxx";`},
		{Name: "Quote/Uni16-EOF", Input: `stmt "\uf`},
		{Name: "Quote/Uni32-Invalid", Input: `stmt "\U12345";`},
		{Name: "Quote/Uni32-Invalid", Input: `stmt "\U12345xx";`},
		{Name: "Quote/Uni32-EOF", Input: `stmt "\U123456`},
		{Name: "Quote/BadEscape", Input: `stmt "\z";`},
		{Name: "Quote/BadUTF8", Input: "stmt \"\xff"},
		// Raw strings -- only really affected by EOF
		{Name: "Raw/EOF", Input: "stmt `"},
		{Name: "Raw/EOF", Input: "stmt `after"},
		{Name: "Raw/EOF", Input: "stmt ```"},
		{Name: "Raw/EOF", Input: "stmt ```after"},
		{Name: "Raw/BadUTF8", Input: "stmt `\xff"},
	}

	for i, c := range cases {
		c.Name = fmt.Sprint(c.Name, "-", i+1)
		if c.Seq == nil {
			c.Seq = invalid
		}
		c.Run(t)
	}
}

func TestIntegers(t *testing.T) {
	defer setlogf(t)()
	neg := big.NewInt(-1234)
	pos := big.NewInt(1234)
	_stmt := tokenCase{Token: Token{Kind: TWord, Raw: []byte("stmt"), Value: "stmt"}}
	tokenSeq{
		_stmt,
		// Negative sign
		_ws, {Token: Token{Kind: TBinary, Value: neg, Raw: []byte("-0b10011010010")}},
		_ws, {Token: Token{Kind: TBinary, Value: neg, Raw: []byte("-0B10011010010")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: neg, Raw: []byte("-2#10011010010")}},
		_ws, {Token: Token{Kind: TOctal, Value: neg, Raw: []byte("-02322")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: neg, Raw: []byte("-8#2322")}},
		_ws, {Token: Token{Kind: TInteger, Value: neg, Raw: []byte("-1234")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: neg, Raw: []byte("-10#00001234")}},
		_ws, {Token: Token{Kind: THex, Value: neg, Raw: []byte("-0x4d2")}},
		_ws, {Token: Token{Kind: THex, Value: neg, Raw: []byte("-0X4D2")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: neg, Raw: []byte("-16#4D2")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: neg, Raw: []byte("-36#ya")}},
		// Positive sign
		_ws, {Token: Token{Kind: TBinary, Value: pos, Raw: []byte("+0b10011010010")}},
		_ws, {Token: Token{Kind: TBinary, Value: pos, Raw: []byte("+0B10011010010")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: pos, Raw: []byte("+2#10011010010")}},
		_ws, {Token: Token{Kind: TOctal, Value: pos, Raw: []byte("+02322")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: pos, Raw: []byte("+8#2322")}},
		_ws, {Token: Token{Kind: TInteger, Value: pos, Raw: []byte("+1234")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: pos, Raw: []byte("+10#1234")}},
		_ws, {Token: Token{Kind: THex, Value: pos, Raw: []byte("+0x4d2")}},
		_ws, {Token: Token{Kind: THex, Value: pos, Raw: []byte("+0X4D2")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: pos, Raw: []byte("+16#4D2")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: pos, Raw: []byte("+36#ya")}},
		// No sign
		_ws, {Token: Token{Kind: TBinary, Value: pos, Raw: []byte("0b10011010010")}},
		_ws, {Token: Token{Kind: TBinary, Value: pos, Raw: []byte("0B10011010010")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: pos, Raw: []byte("2#10011010010")}},
		_ws, {Token: Token{Kind: TOctal, Value: pos, Raw: []byte("02322")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: pos, Raw: []byte("8#2322")}},
		_ws, {Token: Token{Kind: TInteger, Value: pos, Raw: []byte("1234")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: pos, Raw: []byte("10#1234")}},
		_ws, {Token: Token{Kind: THex, Value: pos, Raw: []byte("0x4d2")}},
		_ws, {Token: Token{Kind: THex, Value: pos, Raw: []byte("0X4D2")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: pos, Raw: []byte("16#4D2")}},
		_ws, {Token: Token{Kind: TBaseInt, Value: pos, Raw: []byte("36#ya")}},
		_ws, _semicolon,
		_eof,
	}.Run(t, `stmt
			-0b10011010010
			-0B10011010010
			-2#10011010010
			-02322
			-8#2322
			-1234
			-10#00001234
			-0x4d2
			-0X4D2
			-16#4D2
			-36#ya
			+0b10011010010
			+0B10011010010
			+2#10011010010
			+02322
			+8#2322
			+1234
			+10#1234
			+0x4d2
			+0X4D2
			+16#4D2
			+36#ya
			 0b10011010010
			 0B10011010010
			 2#10011010010
			 02322
			 8#2322
			 1234
			 10#1234
			 0x4d2
			 0X4D2
			 16#4D2
			 36#ya
		;`)

	// Check invalid cases
	badValues := []string{
		`4#`, `4#;`,
		`4x`, `4x;`,
		`4X`, `4X;`,
		`4b`, `4b;`,
		`4B`, `4B;`,
		`06z`,
		`0xfg`,
		`4#15`,
		`0b12`,
		`-`,
		`+`,
	}
	for _, c := range badValues {
		want := strings.TrimRight(c, ";")
		seq := tokenSeq{
			{Token: Token{Kind: TWord, Raw: []byte(want), Value: want}},
		}
		t.Run(c, func(t *testing.T) {
			defer setlogf(t)()
			seq.Run(t, c)
		})
	}
}

func TestRationals(t *testing.T) {
	defer setlogf(t)()
	zero := big.NewRat(0, 1)
	neg := big.NewRat(-3, 4)
	pos := big.NewRat(3, 4)
	tokenSeq{
		{Token: Token{Kind: TWord, Raw: []byte("stmt"), Value: "stmt"}},
		// Zero
		_ws, {Token: Token{Kind: TRational, Value: zero, Raw: []byte("0/100")}},
		_ws, {Token: Token{Kind: TRational, Value: zero, Raw: []byte("-0/200")}},
		_ws, {Token: Token{Kind: TRational, Value: zero, Raw: []byte("-0/1")}},
		_ws, {Token: Token{Kind: TRational, Value: zero, Raw: []byte("0/1")}},
		// Negative sign
		_ws, {Token: Token{Kind: TRational, Value: neg, Raw: []byte("-3/4")}},
		_ws, {Token: Token{Kind: TRational, Value: neg, Raw: []byte("-6/8")}},
		_ws, {Token: Token{Kind: TRational, Value: neg, Raw: []byte("-75/100")}},
		// Positive sign
		_ws, {Token: Token{Kind: TRational, Value: pos, Raw: []byte("+3/4")}},
		_ws, {Token: Token{Kind: TRational, Value: pos, Raw: []byte("+6/8")}},
		_ws, {Token: Token{Kind: TRational, Value: pos, Raw: []byte("+75/100")}},
		// No sign
		_ws, {Token: Token{Kind: TRational, Value: pos, Raw: []byte("3/4")}},
		_ws, {Token: Token{Kind: TRational, Value: pos, Raw: []byte("6/8")}},
		_ws, {Token: Token{Kind: TRational, Value: pos, Raw: []byte("75/100")}},
		_ws, _semicolon,
		_eof,
	}.Run(t, `stmt
			 0/100 -0/200 -0/1 0/1
			-3/4 -6/8 -75/100
			+3/4 +6/8 +75/100
			 3/4 6/8 75/100
		;`)

	// Zero denominator -> error
	tokenSeq{
		{Token: Token{Kind: TWord, Raw: []byte("5/0"), Value: "5/0"}},
	}.Run(t, `5/0`)

	// Fail on EOF in rational (needs a sentinel)
	tokenSeq{
		{Token: Token{Kind: TWord, Raw: []byte("5/"), Value: "5/"}},
	}.Run(t, `5/`)
}

func TestLocationString(t *testing.T) {
	const (
		want      = "2:34:45"
		wantNamed = "Location Name:" + want
	)
	defer setlogf(t)()
	loc := Location{
		Line:   2,
		Column: 34,
		Offset: 45,
	}
	if got := loc.String(); got != want {
		t.Fatalf("%#+v.String() = %q; want %q", loc, got, want)
	}

	loc.Name = "Location Name"
	if got := loc.String(); got != wantNamed {
		t.Fatalf("%#+v.String() = %q; want %q", loc, got, wantNamed)
	}
}

func TestFloats(t *testing.T) {
	defer setlogf(t)()

	_stmt := tokenCase{Token: Token{Kind: TWord, Raw: []byte("stmt"), Value: "stmt"}}
	tokenSeq{
		_stmt,
		// Negative sign
		_ws, floatCase("-0.0", 0),
		_ws, floatCase("-0.5", 0),
		_ws, floatCase("-0.0e0", 0),
		_ws, floatCase("-0.0E0", 0),
		_ws, floatCase("-1.2345", 0),
		_ws, floatCase("-12345e-4", 0),
		_ws, floatCase("-1.2345e4", 0),
		_ws, floatCase("-1.2345e+4", 0),
		// Positive sign
		_ws, floatCase("+0.0", 0),
		_ws, floatCase("+0.5", 0),
		_ws, floatCase("+0.0e0", 0),
		_ws, floatCase("+0.0E0", 0),
		_ws, floatCase("+1.2345", 0),
		_ws, floatCase("+12345E-4", 0),
		_ws, floatCase("+1.2345E4", 0),
		_ws, floatCase("+1.2345E+4", 0),
		// No sign
		_ws, _bracketopen, floatCase("0.0", 0), _bracketclose,
		_ws, _mapopen,
		{Token: Token{Kind: TWord, Value: "k"}},
		_ws, floatCase("0.5", 0),
		_curlclose,
		_ws, floatCase("0e123", 0),
		_ws, floatCase("0.0e0", 0),
		_ws, floatCase("0.0E0", 0),
		_ws, floatCase("1.2345", 0),
		_ws, floatCase("12345e-4", 0),
		_ws, floatCase("1.2345e4", 0),
		_ws, floatCase("1.2345e+4", 0),
		_ws, _semicolon,
		_eof,
	}.Run(t, `stmt
			-0.0 -0.5 -0.0e0 -0.0E0
			-1.2345 -12345e-4 -1.2345e4 -1.2345e+4
			+0.0 +0.5 +0.0e0 +0.0E0
			+1.2345 +12345E-4 +1.2345E4 +1.2345E+4
			[0.0] #{k 0.5} 0e123 0.0e0 0.0E0
			1.2345 12345e-4 1.2345e4 1.2345e+4
		;`)

	// Check invalid cases that turn into words
	badValues := []string{
		`5z`,    // invalid char
		`5ez`,   // invalid char
		`5.`,    // eof
		`5.z`,   // invalid char
		`5e+z`,  // invalid char
		`5e-z`,  // invalid char
		`5e+5z`, // invalid char
		`5e-5z`, // invalid char
		`5e+0z`, // invalid char
		`5e-0z`, // invalid char
		`0.5e`,  // eof
		`0.5e+`, // eof
		`0.5e-`, // eof
	}
	for _, c := range badValues {
		seq := tokenSeq{
			{Token: Token{Kind: TWord, Raw: []byte(c), Value: c}},
		}
		t.Run(c, func(t *testing.T) {
			defer setlogf(t)()
			seq.Run(t, c)
		})
	}
}

func TestDurations(t *testing.T) {
	defer setlogf(t)()

	_stmt := tokenCase{Token: Token{Kind: TWord, Raw: []byte("stmt"), Value: "stmt"}}

	t.Run("Valid", func(t *testing.T) {
		defer setlogf(t)()
		tokenSeq{
			_stmt,
			// Negative sign
			_ws, durCase("-0s"),
			_ws, durCase("-1ns"),
			_ws, durCase("-0ns"),
			_ws, durCase("-0.0s"),
			_ws, durCase("-1h234m7s"),
			_ws, durCase("-1h"),
			_ws, durCase("-60m"),
			_ws, durCase("-0.05s"),
			_ws, durCase("-500ms"),
			_ws, durCase("-0.5ms"),
			_ws, durCase("-500us"),
			_ws, durCase("-500μs"),
			// Positive sign
			_ws, durCase("+0s"),
			_ws, durCase("+1ns"),
			_ws, durCase("+0ns"),
			_ws, durCase("+0.0s"),
			_ws, durCase("+1h234m7s"),
			_ws, durCase("+1h"),
			_ws, durCase("+60m"),
			_ws, durCase("+0.05s"),
			_ws, durCase("+500ms"),
			_ws, durCase("+0.5ms"),
			_ws, durCase("+500us"),
			_ws, durCase("+500μs"),
			// No sign
			_ws, durCase("0s"),
			_ws, durCase("1ns"),
			_ws, durCase("0ns"),
			_ws, durCase("0.0s"),
			_ws, durCase("1h234m7s"),
			_ws, durCase("1h"),
			_ws, durCase("60m"),
			_ws, durCase("0.05s"),
			_ws, durCase("500ms"),
			_ws, durCase("0.5ms"),
			_ws, durCase("500us"),
			_ws, durCase("500μs"),
			_ws, durCase("1h0.25m"),
			_ws, durCase("1h0m0.05s"),
			_ws, _semicolon,
			_eof,
		}.Run(t, `stmt
			-0s -1ns -0ns -0.0s
			-1h234m7s -1h -60m
			-0.05s -500ms
			-0.5ms -500us -500μs
			+0s +1ns +0ns +0.0s
			+1h234m7s +1h +60m
			+0.05s +500ms
			+0.5ms +500us +500μs
			0s 1ns 0ns 0.0s
			1h234m7s 1h 60m
			0.05s 500ms
			0.5ms 500us 500μs
			1h0.25m
			1h0m0.05s
		;`)
	})

	// Check invalid cases -- these become words
	badValues := []string{
		`1mz`,
		`1h0`,
		`1h00`,
		`1h0z`,
		`1h0.0`,
		`1h0.1`,
		`1h0.`,
		`1h0.5`,
		`1h0.5n`,
		`1h0.;`,
		`1h0.5;`,
		`1h0.5n;`,
		`1h0u`,
		`1h0u;`,
		`1h0.5`,
		`1h0.0z`,
	}
	for _, c := range badValues {
		want := strings.TrimRight(c, ";")
		seq := tokenSeq{
			{Token: Token{Kind: TWord, Raw: []byte(want), Value: want}},
		}
		t.Run("Bad/"+c, func(t *testing.T) {
			defer setlogf(t)()
			t.Logf("Parsing %q", c)
			seq.Run(t, c)
		})
	}
}

func TestReaderWrapping(t *testing.T) {
	const path = "_test/simple-file"
	const noname = "no-name"

	want := tokenSeq{
		_comment, _ws,
		_comment, _ws,
		wordCase("statement"), _ws, wordCase("word"), _semicolon, _ws,
		_comment, _ws,
		wordCase("section"), _ws, quoteCase("string"), _ws, _curlopen, _ws,
		_comment, _ws,
		wordCase("url"), _ws, wordCase("https://go.spiff.io/codf"), _semicolon, _ws,
		_curlclose, _ws,
		_comment,
	}

	t.Run("File", func(t *testing.T) {
		fi, err := os.Open(path)
		if err != nil {
			t.Skipf("Cannot open test file; skipping: %v", err)
		}
		defer fi.Close()

		lexer := NewLexer(fi)
		lexer.Name = noname

		want.RunWithLexer(t, lexer)

		if lexer.pos.Name != path {
			t.Fatalf("lexer.pos.Name = %q; want %q", lexer.pos.Name, path)
		}
	})

	t.Run("SimpleReader", func(t *testing.T) {
		fi, err := os.Open(path)
		if err != nil {
			t.Skipf("Cannot open test file; skipping: %v", err)
		}
		defer fi.Close()

		lexer := NewLexer(struct{ io.Reader }{Reader: fi})
		lexer.Name = "no-name"

		want.RunWithLexer(t, lexer)

		if lexer.pos.Name != noname {
			t.Fatalf("lexer.pos.Name = %q; want %q", lexer.pos.Name, noname)
		}
	})
}

func TestLexerPrecision(t *testing.T) {
	seq := `1.234`
	cases := map[string]uint{
		"DefaultPrecision": 0,
		"HighPrecision":    240,
		"32bit":            32,
	}

	for name, prec := range cases {
		prec := prec
		t.Run(name, func(t *testing.T) {
			defer setlogf(t)()
			l := NewLexer(strings.NewReader(seq))
			l.Precision = prec
			tokenSeq{floatCase(seq, prec)}.RunWithLexer(t, l)
		})
	}
}
