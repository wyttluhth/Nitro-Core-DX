package corelx

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

// TokenType represents the type of a token
type TokenType int

const (
	// Special tokens
	TOKEN_EOF TokenType = iota
	TOKEN_NEWLINE
	TOKEN_INDENT
	TOKEN_DEDENT
	TOKEN_ERROR

	// Literals
	TOKEN_IDENTIFIER
	TOKEN_NUMBER
	TOKEN_STRING

	// Keywords
	TOKEN_FUNCTION
	TOKEN_IF
	TOKEN_ELSEIF
	TOKEN_ELSE
	TOKEN_WHILE
	TOKEN_FOR
	TOKEN_RETURN
	TOKEN_TYPE
	TOKEN_STRUCT
	TOKEN_ASSET
	TOKEN_TRUE
	TOKEN_FALSE
	TOKEN_AND
	TOKEN_OR
	TOKEN_NOT
	TOKEN_CONST
	TOKEN_VAR
	TOKEN_AT
	TOKEN_TO
	TOKEN_STEP

	// Operators
	TOKEN_ASSIGN      // :=
	TOKEN_EQUAL       // =
	TOKEN_PLUS        // +
	TOKEN_MINUS       // -
	TOKEN_STAR        // *
	TOKEN_SLASH       // /
	TOKEN_PERCENT     // %
	TOKEN_EQUAL_EQUAL // ==
	TOKEN_BANG_EQUAL  // !=
	TOKEN_LESS        // <
	TOKEN_LESS_EQUAL  // <=
	TOKEN_GREATER     // >
	TOKEN_GREATER_EQUAL // >=
	TOKEN_AMPERSAND   // &
	TOKEN_PIPE        // |
	TOKEN_CARET       // ^
	TOKEN_TILDE       // ~
	TOKEN_LSHIFT      // <<
	TOKEN_RSHIFT      // >>
	TOKEN_ADDR_OF     // &

	// Delimiters
	TOKEN_LPAREN   // (
	TOKEN_RPAREN   // )
	TOKEN_LBRACKET // [
	TOKEN_RBRACKET // ]
	TOKEN_COMMA    // ,
	TOKEN_COLON    // :
	TOKEN_ARROW    // ->
	TOKEN_DOT      // .

	// Comments
	TOKEN_COMMENT
)

// Token represents a lexical token
type Token struct {
	Type     TokenType
	Literal  string
	Line     int
	Column   int
	IndentLevel int // For INDENT/DEDENT tokens
}

// Lexer tokenizes CoreLX source code
type Lexer struct {
	source        string
	position      int
	line          int
	column        int
	indentStack   []int // Stack of indentation levels
	indentPending bool  // Whether we need to emit INDENT/DEDENT
	tokens        []Token
}

// NewLexer creates a new lexer
func NewLexer(source string) *Lexer {
	return &Lexer{
		source:        source,
		position:      0,
		line:          1,
		column:        1,
		indentStack:   []int{0}, // Start with indent level 0
		indentPending: false,
		tokens:        make([]Token, 0),
	}
}

// Tokenize tokenizes the entire source
func (l *Lexer) Tokenize() ([]Token, error) {
	for !l.isAtEnd() {
		if l.indentPending {
			l.handleIndentation()
		}
		l.skipWhitespace()
		if l.isAtEnd() {
			break
		}
		if err := l.scanToken(); err != nil {
			return nil, err
		}
	}

	// Emit final DEDENTs if needed
	for len(l.indentStack) > 1 {
		l.indentStack = l.indentStack[:len(l.indentStack)-1]
		l.tokens = append(l.tokens, Token{
			Type:       TOKEN_DEDENT,
			Line:       l.line,
			Column:     l.column,
			IndentLevel: l.indentStack[len(l.indentStack)-1],
		})
	}

	// Emit EOF
	l.tokens = append(l.tokens, Token{
		Type:   TOKEN_EOF,
		Line:   l.line,
		Column: l.column,
	})

	return l.tokens, nil
}

func (l *Lexer) scanToken() error {
	line := l.line
	column := l.column

	r := l.advance()

	switch r {
	case '\n':
		l.line++
		l.column = 1
		l.indentPending = true
		l.tokens = append(l.tokens, Token{
			Type:   TOKEN_NEWLINE,
			Line:   line,
			Column: column,
		})
		return nil

	case '(':
		l.emitToken(TOKEN_LPAREN, "(", line, column)
		return nil
	case ')':
		l.emitToken(TOKEN_RPAREN, ")", line, column)
		return nil
	case '[':
		l.emitToken(TOKEN_LBRACKET, "[", line, column)
		return nil
	case ']':
		l.emitToken(TOKEN_RBRACKET, "]", line, column)
		return nil
	case ',':
		l.emitToken(TOKEN_COMMA, ",", line, column)
		return nil
	case '.':
		l.emitToken(TOKEN_DOT, ".", line, column)
		return nil
	case ':':
		if l.match('=') {
			l.emitToken(TOKEN_ASSIGN, ":=", line, column)
		} else {
			l.emitToken(TOKEN_COLON, ":", line, column)
		}
		return nil
	case '=':
		if l.match('=') {
			l.emitToken(TOKEN_EQUAL_EQUAL, "==", line, column)
		} else {
			l.emitToken(TOKEN_EQUAL, "=", line, column)
		}
		return nil
	case '!':
		if l.match('=') {
			l.emitToken(TOKEN_BANG_EQUAL, "!=", line, column)
		} else {
			return l.error(line, column, "Unexpected character: !")
		}
		return nil
	case '<':
		if l.match('=') {
			l.emitToken(TOKEN_LESS_EQUAL, "<=", line, column)
		} else if l.match('<') {
			l.emitToken(TOKEN_LSHIFT, "<<", line, column)
		} else {
			l.emitToken(TOKEN_LESS, "<", line, column)
		}
		return nil
	case '>':
		if l.match('=') {
			l.emitToken(TOKEN_GREATER_EQUAL, ">=", line, column)
		} else if l.match('>') {
			l.emitToken(TOKEN_RSHIFT, ">>", line, column)
		} else {
			l.emitToken(TOKEN_GREATER, ">", line, column)
		}
		return nil
	case '+':
		l.emitToken(TOKEN_PLUS, "+", line, column)
		return nil
	case '-':
		if l.match('>') {
			l.emitToken(TOKEN_ARROW, "->", line, column)
		} else if l.match('-') {
			// Comment: --
			return l.scanComment(line, column)
		} else {
			l.emitToken(TOKEN_MINUS, "-", line, column)
		}
		return nil
	case '*':
		l.emitToken(TOKEN_STAR, "*", line, column)
		return nil
	case '/':
		l.emitToken(TOKEN_SLASH, "/", line, column)
		return nil
	case '%':
		l.emitToken(TOKEN_PERCENT, "%", line, column)
		return nil
	case '&':
		l.emitToken(TOKEN_AMPERSAND, "&", line, column)
		return nil
	case '|':
		l.emitToken(TOKEN_PIPE, "|", line, column)
		return nil
	case '^':
		l.emitToken(TOKEN_CARET, "^", line, column)
		return nil
	case '~':
		l.emitToken(TOKEN_TILDE, "~", line, column)
		return nil

	case '"':
		return l.scanString(line, column)

	default:
		if unicode.IsDigit(r) {
			l.position--
			l.column--
			return l.scanNumber(line, column)
		}
		if unicode.IsLetter(r) || r == '_' {
			l.position--
			l.column--
			return l.scanIdentifier(line, column)
		}
		return l.error(line, column, fmt.Sprintf("Unexpected character: %c", r))
	}
}

func (l *Lexer) scanComment(line, column int) error {
	// Skip until end of line
	for !l.isAtEnd() && l.peek() != '\n' {
		l.advance()
	}
	return nil
}

func (l *Lexer) scanString(line, column int) error {
	start := l.position
	for !l.isAtEnd() {
		r := l.advance()
		if r == '"' {
			literal := l.source[start-1 : l.position]
			l.emitToken(TOKEN_STRING, literal, line, column)
			return nil
		}
		if r == '\n' {
			return l.error(line, column, "Unterminated string")
		}
	}
	return l.error(line, column, "Unterminated string")
}

func (l *Lexer) scanNumber(line, column int) error {
	start := l.position

	// Check for hex prefix
	if l.match('0') && l.match('x') {
		for unicode.IsDigit(l.peek()) || (l.peek() >= 'a' && l.peek() <= 'f') || (l.peek() >= 'A' && l.peek() <= 'F') {
			l.advance()
		}
	} else {
		// Decimal number, optionally with a fractional part (fixed-point
		// literal, charter D4: decimal literals are 8.8 fixed).
		for unicode.IsDigit(l.peek()) {
			l.advance()
		}
		if l.peek() == '.' && unicode.IsDigit(l.peekNext()) {
			l.advance() // consume '.'
			for unicode.IsDigit(l.peek()) {
				l.advance()
			}
		}
	}

	literal := l.source[start:l.position]
	l.emitToken(TOKEN_NUMBER, literal, line, column)
	return nil
}

func (l *Lexer) scanIdentifier(line, column int) error {
	start := l.position
	for unicode.IsLetter(l.peek()) || unicode.IsDigit(l.peek()) || l.peek() == '_' {
		l.advance()
	}

	literal := l.source[start:l.position]
	tokenType := l.identifierType(literal)
	l.emitToken(tokenType, literal, line, column)
	return nil
}

func (l *Lexer) identifierType(literal string) TokenType {
	keywords := map[string]TokenType{
		"function": TOKEN_FUNCTION,
		"if":       TOKEN_IF,
		"elseif":   TOKEN_ELSEIF,
		"else":     TOKEN_ELSE,
		"while":    TOKEN_WHILE,
		"for":      TOKEN_FOR,
		"return":   TOKEN_RETURN,
		"type":     TOKEN_TYPE,
		"struct":   TOKEN_STRUCT,
		"asset":    TOKEN_ASSET,
		"true":     TOKEN_TRUE,
		"false":    TOKEN_FALSE,
		"and":      TOKEN_AND,
		"or":       TOKEN_OR,
		"not":      TOKEN_NOT,
		"const":    TOKEN_CONST,
		"var":      TOKEN_VAR,
		"at":       TOKEN_AT,
		"to":       TOKEN_TO,
		"step":     TOKEN_STEP,
	}
	if tokenType, ok := keywords[literal]; ok {
		return tokenType
	}
	return TOKEN_IDENTIFIER
}

func (l *Lexer) handleIndentation() {
	l.indentPending = false

	// Skip whitespace to determine indentation
	indent := 0
	hasTabs := false
	hasSpaces := false

	for !l.isAtEnd() {
		r := l.peek()
		if r == ' ' {
			indent++
			hasSpaces = true
			l.advance()
		} else if r == '\t' {
			indent++
			hasTabs = true
			l.advance()
		} else if r == '\n' {
			// Empty line, reset and continue
			indent = 0
			l.advance()
			l.line++
			l.column = 1
			continue
		} else {
			break
		}
	}

	// Check for mixed tabs and spaces
	if hasTabs && hasSpaces {
		l.tokens = append(l.tokens, Token{
			Type:   TOKEN_ERROR,
			Literal: fmt.Sprintf("Mixed tabs and spaces at line %d", l.line),
			Line:   l.line,
			Column: 1,
		})
		return
	}

	currentIndent := l.indentStack[len(l.indentStack)-1]

	if indent > currentIndent {
		// Indent increased
		l.indentStack = append(l.indentStack, indent)
		l.tokens = append(l.tokens, Token{
			Type:       TOKEN_INDENT,
			Line:       l.line,
			Column:     1,
			IndentLevel: indent,
		})
	} else if indent < currentIndent {
		// Indent decreased - emit DEDENTs until we match
		for len(l.indentStack) > 1 && indent < l.indentStack[len(l.indentStack)-1] {
			l.indentStack = l.indentStack[:len(l.indentStack)-1]
			l.tokens = append(l.tokens, Token{
				Type:       TOKEN_DEDENT,
				Line:       l.line,
				Column:     1,
				IndentLevel: l.indentStack[len(l.indentStack)-1],
			})
		}
		// Check if indent matches exactly
		if indent != l.indentStack[len(l.indentStack)-1] {
			l.tokens = append(l.tokens, Token{
				Type:   TOKEN_ERROR,
				Literal: fmt.Sprintf("Indentation mismatch at line %d: expected %d, got %d", l.line, l.indentStack[len(l.indentStack)-1], indent),
				Line:   l.line,
				Column: 1,
			})
		}
	}
	// If indent == currentIndent, no INDENT/DEDENT needed
}

func (l *Lexer) skipWhitespace() {
	for !l.isAtEnd() {
		r := l.peek()
		if r == ' ' || r == '\t' || r == '\r' {
			l.advance()
		} else {
			break
		}
	}
}

func (l *Lexer) advance() rune {
	if l.isAtEnd() {
		return 0
	}
	r, size := utf8.DecodeRuneInString(l.source[l.position:])
	l.position += size
	l.column += size
	return r
}

func (l *Lexer) peek() rune {
	if l.isAtEnd() {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.source[l.position:])
	return r
}

func (l *Lexer) match(expected rune) bool {
	if l.isAtEnd() {
		return false
	}
	r, size := utf8.DecodeRuneInString(l.source[l.position:])
	if r != expected {
		return false
	}
	l.position += size
	l.column += size
	return true
}

func (l *Lexer) isAtEnd() bool {
	return l.position >= len(l.source)
}

func (l *Lexer) emitToken(tokenType TokenType, literal string, line, column int) {
	l.tokens = append(l.tokens, Token{
		Type:    tokenType,
		Literal: literal,
		Line:    line,
		Column:  column,
	})
}

func (l *Lexer) error(line, column int, message string) error {
	l.tokens = append(l.tokens, Token{
		Type:    TOKEN_ERROR,
		Literal: message,
		Line:    line,
		Column:  column,
	})
	return fmt.Errorf("lexer error at line %d, column %d: %s", line, column, message)
}

// peekNext returns the rune after the current position without advancing.
func (l *Lexer) peekNext() rune {
	if l.isAtEnd() {
		return 0
	}
	_, size := utf8.DecodeRuneInString(l.source[l.position:])
	if l.position+size >= len(l.source) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.source[l.position+size:])
	return r
}
