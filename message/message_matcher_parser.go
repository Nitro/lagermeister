//line message_matcher_parser.y:2
package message

import __yyfmt__ "fmt"

//line message_matcher_parser.y:2
import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"sync"
	"unicode/utf8"
)

const (
	STARTS_WITH = 1
	ENDS_WITH   = 2
)

var variables = map[string]int{
	"Uuid":       VAR_UUID,
	"Type":       VAR_TYPE,
	"Logger":     VAR_LOGGER,
	"Payload":    VAR_PAYLOAD,
	"EnvVersion": VAR_ENVVERSION,
	"Hostname":   VAR_HOSTNAME,
	"Timestamp":  VAR_TIMESTAMP,
	"Severity":   VAR_SEVERITY,
	"Pid":        VAR_PID,
	"Fields":     VAR_FIELDS,
	"TRUE":       TRUE,
	"FALSE":      FALSE,
	"NIL":        NIL_VALUE}

var parseLock sync.Mutex

type Statement struct {
	field, op, value yySymType
}

type tree struct {
	left  *tree
	stmt  *Statement
	right *tree
}

type stack struct {
	top  *item
	size int
}

type item struct {
	node *tree
	next *item
}

func (s *stack) push(node *tree) {
	s.top = &item{node, s.top}
	s.size++
}

func (s *stack) pop() (node *tree) {
	if s.size > 0 {
		node, s.top = s.top.node, s.top.next
		s.size--
		return
	}
	return nil
}

var nodes []*tree

//line message_matcher_parser.y:73
type yySymType struct {
	yys        int
	tokenId    int
	token      string
	double     float64
	fieldIndex int
	arrayIndex int
	regexp     *regexp.Regexp
}

const OP_EQ = 57346
const OP_NE = 57347
const OP_GT = 57348
const OP_GTE = 57349
const OP_LT = 57350
const OP_LTE = 57351
const OP_RE = 57352
const OP_NRE = 57353
const OP_OR = 57354
const OP_AND = 57355
const VAR_UUID = 57356
const VAR_TYPE = 57357
const VAR_LOGGER = 57358
const VAR_PAYLOAD = 57359
const VAR_ENVVERSION = 57360
const VAR_HOSTNAME = 57361
const VAR_TIMESTAMP = 57362
const VAR_SEVERITY = 57363
const VAR_PID = 57364
const VAR_FIELDS = 57365
const STRING_VALUE = 57366
const NUMERIC_VALUE = 57367
const REGEXP_VALUE = 57368
const NIL_VALUE = 57369
const TRUE = 57370
const FALSE = 57371

var yyToknames = [...]string{
	"$end",
	"error",
	"$unk",
	"OP_EQ",
	"OP_NE",
	"OP_GT",
	"OP_GTE",
	"OP_LT",
	"OP_LTE",
	"OP_RE",
	"OP_NRE",
	"OP_OR",
	"OP_AND",
	"VAR_UUID",
	"VAR_TYPE",
	"VAR_LOGGER",
	"VAR_PAYLOAD",
	"VAR_ENVVERSION",
	"VAR_HOSTNAME",
	"VAR_TIMESTAMP",
	"VAR_SEVERITY",
	"VAR_PID",
	"VAR_FIELDS",
	"STRING_VALUE",
	"NUMERIC_VALUE",
	"REGEXP_VALUE",
	"NIL_VALUE",
	"TRUE",
	"FALSE",
	"'('",
	"')'",
}
var yyStatenames = [...]string{}

const yyEofCode = 1
const yyErrCode = 2
const yyInitialStackSize = 16

//line message_matcher_parser.y:191
type MatcherSpecificationParser struct {
	spec     string
	sym      string
	peekrune rune
	lexPos   int
	reToken  *regexp.Regexp
}

func parseMatcherSpecification(ms *MatcherSpecification) error {
	parseLock.Lock()
	defer parseLock.Unlock()
	nodes = nodes[:0] // reset the global
	var msp MatcherSpecificationParser
	msp.spec = ms.spec
	msp.peekrune = ' '
	msp.reToken, _ = regexp.Compile("%[A-Z]+%")
	if yyParse(&msp) == 0 {
		s := new(stack)
		for _, node := range nodes {
			if node.stmt.op.tokenId != OP_OR &&
				node.stmt.op.tokenId != OP_AND {
				s.push(node)
			} else {
				node.right = s.pop()
				node.left = s.pop()
				s.push(node)
			}
		}
		ms.vm = s.pop()
		return nil
	}
	return fmt.Errorf("syntax error: last token: %s pos: %d", msp.sym, msp.lexPos)
}

func (m *MatcherSpecificationParser) Error(s string) {
	fmt.Errorf("syntax error: %s last token: %s pos: %d", m.sym, m.lexPos)
}

func (m *MatcherSpecificationParser) Lex(yylval *yySymType) int {
	var err error
	var c, tmp rune
	var i int

	yylval.tokenId = 0
	yylval.token = ""
	yylval.double = 0
	yylval.fieldIndex = 0
	yylval.arrayIndex = 0
	yylval.regexp = nil

	c = m.peekrune
	m.peekrune = ' '

loop:
	if c >= 'A' && c <= 'Z' {
		goto variable
	}
	if (c >= '0' && c <= '9') || c == '.' {
		goto number
	}
	switch c {
	case ' ', '\t':
		c = m.getrune()
		goto loop
	case '=':
		c = m.getrune()
		if c == '=' {
			yylval.token = "=="
			yylval.tokenId = OP_EQ
		} else if c == '~' {
			yylval.token = "=~"
			yylval.tokenId = OP_RE
		} else {
			break
		}
		return yylval.tokenId
	case '!':
		c = m.getrune()
		if c == '=' {
			yylval.token = "!="
			yylval.tokenId = OP_NE
		} else if c == '~' {
			yylval.token = "!~"
			yylval.tokenId = OP_NRE
		} else {
			break
		}
		return yylval.tokenId
	case '>':
		c = m.getrune()
		if c != '=' {
			m.peekrune = c
			yylval.token = ">"
			yylval.tokenId = OP_GT
			return yylval.tokenId
		}
		yylval.token = ">="
		yylval.tokenId = OP_GTE
		return yylval.tokenId
	case '<':
		c = m.getrune()
		if c != '=' {
			m.peekrune = c
			yylval.token = "<"
			yylval.tokenId = OP_LT
			return yylval.tokenId
		}
		yylval.token = "<="
		yylval.tokenId = OP_LTE
		return yylval.tokenId
	case '|':
		c = m.getrune()
		if c != '|' {
			break
		}
		yylval.token = "||"
		yylval.tokenId = OP_OR
		return yylval.tokenId
	case '&':
		c = m.getrune()
		if c != '&' {
			break
		}
		yylval.token = "&&"
		yylval.tokenId = OP_AND
		return yylval.tokenId
	case '"', '\'':
		goto quotestring
	case '/':
		goto regexpstring
	}
	return int(c)

variable:
	m.sym = ""
	for i = 0; ; i++ {
		m.sym += string(c)
		c = m.getrune()
		if !rvariable(c) {
			break
		}
	}
	yylval.tokenId = variables[m.sym]
	if yylval.tokenId == VAR_FIELDS {
		if c != '[' {
			return 0
		}
		var bracketCount int
		var idx [3]string
		for {
			c = m.getrune()
			if c == 0 {
				return 0
			}
			if c == ']' { // a closing bracket in the variable name will fail validation
				if len(idx[bracketCount]) == 0 {
					return 0
				}
				bracketCount++
				m.peekrune = m.getrune()
				if m.peekrune == '[' && bracketCount < cap(idx) {
					m.peekrune = ' '
				} else {
					break
				}
			} else {
				switch bracketCount {
				case 0:
					idx[bracketCount] += string(c)
				case 1, 2:
					if ddigit(c) {
						idx[bracketCount] += string(c)
					} else {
						return 0
					}
				}
			}
		}
		if len(idx[1]) == 0 {
			idx[1] = "0"
		}
		if len(idx[2]) == 0 {
			idx[2] = "0"
		}
		var err error
		yylval.token = idx[0]
		yylval.fieldIndex, err = strconv.Atoi(idx[1])
		if err != nil {
			return 0
		}
		yylval.arrayIndex, err = strconv.Atoi(idx[2])
		if err != nil {
			return 0
		}
	} else {
		yylval.token = m.sym
		m.peekrune = c
	}
	return yylval.tokenId

number:
	m.sym = ""
	for i = 0; ; i++ {
		m.sym += string(c)
		c = m.getrune()
		if !rdigit(c) {
			break
		}
	}
	m.peekrune = c
	yylval.double, err = strconv.ParseFloat(m.sym, 64)
	if err != nil {
		log.Printf("error converting %v\n", m.sym)
		yylval.double = 0
	}
	yylval.token = m.sym
	yylval.tokenId = NUMERIC_VALUE
	return yylval.tokenId

quotestring:
	tmp = c
	m.sym = ""
	for {
		c = m.getrune()
		if c == 0 {
			return 0
		}
		if c == '\\' {
			m.peekrune = m.getrune()
			if m.peekrune == tmp {
				m.sym += string(tmp)
			} else {
				m.sym += string(c)
				m.sym += string(m.peekrune)
			}
			m.peekrune = ' '
			continue
		}
		if c == tmp {
			break
		}
		m.sym += string(c)
	}
	yylval.token = m.sym
	yylval.tokenId = STRING_VALUE
	return yylval.tokenId

regexpstring:
	m.sym = ""
	for {
		c = m.getrune()
		if c == 0 {
			return 0
		}
		if c == '\\' {
			m.peekrune = m.getrune()
			if m.peekrune == '/' {
				m.sym += "/"
			} else {
				m.sym += string(c)
				m.sym += string(m.peekrune)
			}
			m.peekrune = ' '
			continue
		}
		if c == '/' {
			break
		}
		m.sym += string(c)
	}
	rlen := len(m.sym)
	if rlen > 0 && m.sym[0] == '^' {
		if re, err := regexp.Compile(m.sym[1:]); err == nil {
			if s, b := re.LiteralPrefix(); b {
				yylval.token = s
				yylval.fieldIndex = STARTS_WITH
				yylval.tokenId = REGEXP_VALUE
				return yylval.tokenId
			}
		}
	}
	if rlen > 0 && m.sym[rlen-1] == '$' {
		if re, err := regexp.Compile(m.sym[:rlen-1]); err == nil {
			if s, b := re.LiteralPrefix(); b {
				yylval.token = s
				yylval.fieldIndex = ENDS_WITH
				yylval.tokenId = REGEXP_VALUE
				return yylval.tokenId
			}
		}
	}
	yylval.regexp, err = regexp.Compile(m.sym)
	if err != nil {
		log.Printf("invalid regexp %v\n", m.sym)
		return 0
	}
	yylval.token = m.sym
	yylval.tokenId = REGEXP_VALUE
	return yylval.tokenId
}

func rvariable(c rune) bool {
	if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
		return true
	}
	return false
}

func rdigit(c rune) bool {
	switch c {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		'.', 'e', '+', '-':
		return true
	}
	return false
}

func ddigit(c rune) bool {
	switch c {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

func (m *MatcherSpecificationParser) getrune() rune {
	var c rune
	var n int

	if m.lexPos >= len(m.spec) {
		return 0
	}
	c, n = utf8.DecodeRuneInString(m.spec[m.lexPos:len(m.spec)])
	m.lexPos += n
	if c == '\n' {
		c = 0
	}
	return c
}

//line yacctab:1
var yyExca = [...]int{
	-1, 1,
	1, -1,
	-2, 0,
	-1, 38,
	27, 9,
	-2, 3,
	-1, 41,
	27, 10,
	-2, 4,
}

const yyNprod = 39
const yyPrivate = 57344

var yyTokenNames []string
var yyStates []string

const yyLast = 77

var yyAct = [...]int{

	13, 14, 15, 16, 17, 18, 19, 20, 21, 10,
	7, 24, 23, 51, 11, 12, 3, 11, 12, 2,
	52, 22, 46, 25, 49, 48, 47, 45, 24, 23,
	44, 38, 41, 30, 31, 32, 33, 34, 35, 23,
	6, 5, 4, 42, 43, 9, 8, 40, 1, 50,
	28, 29, 30, 31, 32, 33, 34, 35, 28, 29,
	30, 31, 32, 33, 26, 27, 0, 0, 0, 0,
	0, 0, 0, 0, 36, 37, 39,
}
var yyPact = [...]int{

	-14, -14, 16, -14, -1000, -1000, -1000, -1000, 46, 54,
	27, -1000, -1000, -1000, -1000, -1000, -1000, -1000, -1000, -1000,
	-1000, -1000, 16, -14, -14, -1, 3, -4, -1000, -1000,
	-1000, -1000, -1000, -1000, -1000, -1000, 1, 0, -11, -13,
	-7, -1000, -1000, 26, -1000, -1000, -1000, -1000, -1000, -1000,
	-1000, -1000, -1000,
}
var yyPgo = [...]int{

	0, 48, 19, 64, 47, 65, 46, 45, 42, 41,
	40, 10,
}
var yyR1 = [...]int{

	0, 1, 1, 3, 3, 3, 3, 3, 3, 4,
	4, 5, 5, 6, 6, 6, 6, 6, 6, 7,
	7, 7, 8, 8, 9, 10, 10, 10, 10, 10,
	11, 11, 2, 2, 2, 2, 2, 2, 2,
}
var yyR2 = [...]int{

	0, 1, 2, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 3, 3, 3, 3, 3, 3, 3, 3,
	1, 1, 3, 3, 3, 1, 1, 1, 1,
}
var yyChk = [...]int{

	-1000, -1, -2, 30, -8, -9, -10, -11, -6, -7,
	23, 28, 29, 14, 15, 16, 17, 18, 19, 20,
	21, 22, -2, 13, 12, -2, -3, -5, 4, 5,
	6, 7, 8, 9, 10, 11, -3, -3, 4, -5,
	-4, 5, -2, -2, 31, 24, 26, 25, 25, 24,
	-11, 26, 27,
}
var yyDef = [...]int{

	0, -2, 1, 0, 35, 36, 37, 38, 0, 0,
	0, 30, 31, 13, 14, 15, 16, 17, 18, 19,
	20, 21, 2, 0, 0, 0, 0, 0, 3, 4,
	5, 6, 7, 8, 11, 12, 0, 0, -2, 0,
	0, -2, 33, 34, 32, 22, 23, 24, 25, 26,
	27, 28, 29,
}
var yyTok1 = [...]int{

	1, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	30, 31,
}
var yyTok2 = [...]int{

	2, 3, 4, 5, 6, 7, 8, 9, 10, 11,
	12, 13, 14, 15, 16, 17, 18, 19, 20, 21,
	22, 23, 24, 25, 26, 27, 28, 29,
}
var yyTok3 = [...]int{
	0,
}

var yyErrorMessages = [...]struct {
	state int
	token int
	msg   string
}{}

//line yaccpar:1

/*	parser for yacc output	*/

var (
	yyDebug        = 0
	yyErrorVerbose = false
)

type yyLexer interface {
	Lex(lval *yySymType) int
	Error(s string)
}

type yyParser interface {
	Parse(yyLexer) int
	Lookahead() int
}

type yyParserImpl struct {
	lval  yySymType
	stack [yyInitialStackSize]yySymType
	char  int
}

func (p *yyParserImpl) Lookahead() int {
	return p.char
}

func yyNewParser() yyParser {
	return &yyParserImpl{}
}

const yyFlag = -1000

func yyTokname(c int) string {
	if c >= 1 && c-1 < len(yyToknames) {
		if yyToknames[c-1] != "" {
			return yyToknames[c-1]
		}
	}
	return __yyfmt__.Sprintf("tok-%v", c)
}

func yyStatname(s int) string {
	if s >= 0 && s < len(yyStatenames) {
		if yyStatenames[s] != "" {
			return yyStatenames[s]
		}
	}
	return __yyfmt__.Sprintf("state-%v", s)
}

func yyErrorMessage(state, lookAhead int) string {
	const TOKSTART = 4

	if !yyErrorVerbose {
		return "syntax error"
	}

	for _, e := range yyErrorMessages {
		if e.state == state && e.token == lookAhead {
			return "syntax error: " + e.msg
		}
	}

	res := "syntax error: unexpected " + yyTokname(lookAhead)

	// To match Bison, suggest at most four expected tokens.
	expected := make([]int, 0, 4)

	// Look for shiftable tokens.
	base := yyPact[state]
	for tok := TOKSTART; tok-1 < len(yyToknames); tok++ {
		if n := base + tok; n >= 0 && n < yyLast && yyChk[yyAct[n]] == tok {
			if len(expected) == cap(expected) {
				return res
			}
			expected = append(expected, tok)
		}
	}

	if yyDef[state] == -2 {
		i := 0
		for yyExca[i] != -1 || yyExca[i+1] != state {
			i += 2
		}

		// Look for tokens that we accept or reduce.
		for i += 2; yyExca[i] >= 0; i += 2 {
			tok := yyExca[i]
			if tok < TOKSTART || yyExca[i+1] == 0 {
				continue
			}
			if len(expected) == cap(expected) {
				return res
			}
			expected = append(expected, tok)
		}

		// If the default action is to accept or reduce, give up.
		if yyExca[i+1] != 0 {
			return res
		}
	}

	for i, tok := range expected {
		if i == 0 {
			res += ", expecting "
		} else {
			res += " or "
		}
		res += yyTokname(tok)
	}
	return res
}

func yylex1(lex yyLexer, lval *yySymType) (char, token int) {
	token = 0
	char = lex.Lex(lval)
	if char <= 0 {
		token = yyTok1[0]
		goto out
	}
	if char < len(yyTok1) {
		token = yyTok1[char]
		goto out
	}
	if char >= yyPrivate {
		if char < yyPrivate+len(yyTok2) {
			token = yyTok2[char-yyPrivate]
			goto out
		}
	}
	for i := 0; i < len(yyTok3); i += 2 {
		token = yyTok3[i+0]
		if token == char {
			token = yyTok3[i+1]
			goto out
		}
	}

out:
	if token == 0 {
		token = yyTok2[1] /* unknown char */
	}
	if yyDebug >= 3 {
		__yyfmt__.Printf("lex %s(%d)\n", yyTokname(token), uint(char))
	}
	return char, token
}

func yyParse(yylex yyLexer) int {
	return yyNewParser().Parse(yylex)
}

func (yyrcvr *yyParserImpl) Parse(yylex yyLexer) int {
	var yyn int
	var yyVAL yySymType
	var yyDollar []yySymType
	_ = yyDollar // silence set and not used
	yyS := yyrcvr.stack[:]

	Nerrs := 0   /* number of errors */
	Errflag := 0 /* error recovery flag */
	yystate := 0
	yyrcvr.char = -1
	yytoken := -1 // yyrcvr.char translated into internal numbering
	defer func() {
		// Make sure we report no lookahead when not parsing.
		yystate = -1
		yyrcvr.char = -1
		yytoken = -1
	}()
	yyp := -1
	goto yystack

ret0:
	return 0

ret1:
	return 1

yystack:
	/* put a state and value onto the stack */
	if yyDebug >= 4 {
		__yyfmt__.Printf("char %v in %v\n", yyTokname(yytoken), yyStatname(yystate))
	}

	yyp++
	if yyp >= len(yyS) {
		nyys := make([]yySymType, len(yyS)*2)
		copy(nyys, yyS)
		yyS = nyys
	}
	yyS[yyp] = yyVAL
	yyS[yyp].yys = yystate

yynewstate:
	yyn = yyPact[yystate]
	if yyn <= yyFlag {
		goto yydefault /* simple state */
	}
	if yyrcvr.char < 0 {
		yyrcvr.char, yytoken = yylex1(yylex, &yyrcvr.lval)
	}
	yyn += yytoken
	if yyn < 0 || yyn >= yyLast {
		goto yydefault
	}
	yyn = yyAct[yyn]
	if yyChk[yyn] == yytoken { /* valid shift */
		yyrcvr.char = -1
		yytoken = -1
		yyVAL = yyrcvr.lval
		yystate = yyn
		if Errflag > 0 {
			Errflag--
		}
		goto yystack
	}

yydefault:
	/* default state action */
	yyn = yyDef[yystate]
	if yyn == -2 {
		if yyrcvr.char < 0 {
			yyrcvr.char, yytoken = yylex1(yylex, &yyrcvr.lval)
		}

		/* look through exception table */
		xi := 0
		for {
			if yyExca[xi+0] == -1 && yyExca[xi+1] == yystate {
				break
			}
			xi += 2
		}
		for xi += 2; ; xi += 2 {
			yyn = yyExca[xi+0]
			if yyn < 0 || yyn == yytoken {
				break
			}
		}
		yyn = yyExca[xi+1]
		if yyn < 0 {
			goto ret0
		}
	}
	if yyn == 0 {
		/* error ... attempt to resume parsing */
		switch Errflag {
		case 0: /* brand new error */
			yylex.Error(yyErrorMessage(yystate, yytoken))
			Nerrs++
			if yyDebug >= 1 {
				__yyfmt__.Printf("%s", yyStatname(yystate))
				__yyfmt__.Printf(" saw %s\n", yyTokname(yytoken))
			}
			fallthrough

		case 1, 2: /* incompletely recovered error ... try again */
			Errflag = 3

			/* find a state where "error" is a legal shift action */
			for yyp >= 0 {
				yyn = yyPact[yyS[yyp].yys] + yyErrCode
				if yyn >= 0 && yyn < yyLast {
					yystate = yyAct[yyn] /* simulate a shift of "error" */
					if yyChk[yystate] == yyErrCode {
						goto yystack
					}
				}

				/* the current p has no shift on "error", pop stack */
				if yyDebug >= 2 {
					__yyfmt__.Printf("error recovery pops state %d\n", yyS[yyp].yys)
				}
				yyp--
			}
			/* there is no state on the stack with an error shift ... abort */
			goto ret1

		case 3: /* no shift yet; clobber input char */
			if yyDebug >= 2 {
				__yyfmt__.Printf("error recovery discards %s\n", yyTokname(yytoken))
			}
			if yytoken == yyEofCode {
				goto ret1
			}
			yyrcvr.char = -1
			yytoken = -1
			goto yynewstate /* try again in the same state */
		}
	}

	/* reduction by production yyn */
	if yyDebug >= 2 {
		__yyfmt__.Printf("reduce %v in:\n\t%v\n", yyn, yyStatname(yystate))
	}

	yynt := yyn
	yypt := yyp
	_ = yypt // guard against "declared and not used"

	yyp -= yyR2[yyn]
	// yyp is now the index of $0. Perform the default action. Iff the
	// reduced production is ε, $1 is possibly out of range.
	if yyp+1 >= len(yyS) {
		nyys := make([]yySymType, len(yyS)*2)
		copy(nyys, yyS)
		yyS = nyys
	}
	yyVAL = yyS[yyp+1]

	/* consult goto table to find next state */
	yyn = yyR1[yyn]
	yyg := yyPgo[yyn]
	yyj := yyg + yyS[yyp].yys + 1

	if yyj >= yyLast {
		yystate = yyAct[yyg]
	} else {
		yystate = yyAct[yyj]
		if yyChk[yystate] != -yyn {
			yystate = yyAct[yyg]
		}
	}
	// dummy call; replaced with literal code
	switch yynt {

	case 22:
		yyDollar = yyS[yypt-3 : yypt+1]
		//line message_matcher_parser.y:124
		{
			//fmt.Println("string_test", $1, $2, $3)
			nodes = append(nodes, &tree{stmt: &Statement{yyDollar[1], yyDollar[2], yyDollar[3]}})
		}
	case 23:
		yyDollar = yyS[yypt-3 : yypt+1]
		//line message_matcher_parser.y:129
		{
			//fmt.Println("string_test regexp", $1, $2, $3)
			nodes = append(nodes, &tree{stmt: &Statement{yyDollar[1], yyDollar[2], yyDollar[3]}})
		}
	case 24:
		yyDollar = yyS[yypt-3 : yypt+1]
		//line message_matcher_parser.y:135
		{
			//fmt.Println("numeric_test", $1, $2, $3)
			nodes = append(nodes, &tree{stmt: &Statement{yyDollar[1], yyDollar[2], yyDollar[3]}})
		}
	case 25:
		yyDollar = yyS[yypt-3 : yypt+1]
		//line message_matcher_parser.y:141
		{
			//fmt.Println("field_test numeric", $1, $2, $3)
			nodes = append(nodes, &tree{stmt: &Statement{yyDollar[1], yyDollar[2], yyDollar[3]}})
		}
	case 26:
		yyDollar = yyS[yypt-3 : yypt+1]
		//line message_matcher_parser.y:146
		{
			//fmt.Println("field_test string", $1, $2, $3)
			nodes = append(nodes, &tree{stmt: &Statement{yyDollar[1], yyDollar[2], yyDollar[3]}})
		}
	case 27:
		yyDollar = yyS[yypt-3 : yypt+1]
		//line message_matcher_parser.y:151
		{
			//fmt.Println("field_test boolean", $1, $2, $3)
			nodes = append(nodes, &tree{stmt: &Statement{yyDollar[1], yyDollar[2], yyDollar[3]}})
		}
	case 28:
		yyDollar = yyS[yypt-3 : yypt+1]
		//line message_matcher_parser.y:156
		{
			//fmt.Println("field_test regexp", $1, $2, $3)
			nodes = append(nodes, &tree{stmt: &Statement{yyDollar[1], yyDollar[2], yyDollar[3]}})
		}
	case 29:
		yyDollar = yyS[yypt-3 : yypt+1]
		//line message_matcher_parser.y:161
		{
			//fmt.Println("field_test existence", $1, $2, $3)
			nodes = append(nodes, &tree{stmt: &Statement{yyDollar[1], yyDollar[2], yyDollar[3]}})
		}
	case 32:
		yyDollar = yyS[yypt-3 : yypt+1]
		//line message_matcher_parser.y:168
		{
			yyVAL = yyDollar[2]
		}
	case 33:
		yyDollar = yyS[yypt-3 : yypt+1]
		//line message_matcher_parser.y:172
		{
			//fmt.Println("and", $1, $2, $3)
			nodes = append(nodes, &tree{stmt: &Statement{op: yyDollar[2]}})
		}
	case 34:
		yyDollar = yyS[yypt-3 : yypt+1]
		//line message_matcher_parser.y:177
		{
			//fmt.Println("or", $1, $2, $3)
			nodes = append(nodes, &tree{stmt: &Statement{op: yyDollar[2]}})
		}
	case 38:
		yyDollar = yyS[yypt-1 : yypt+1]
		//line message_matcher_parser.y:185
		{
			//fmt.Println("boolean", $1)
			nodes = append(nodes, &tree{stmt: &Statement{op: yyDollar[1]}})
		}
	}
	goto yystack /* stack new state and value */
}
