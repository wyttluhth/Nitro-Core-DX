package corelx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Parser parses CoreLX source code into an AST
type Parser struct {
	tokens  []Token
	current int
}

// NewParser creates a new parser
func NewParser(tokens []Token) *Parser {
	return &Parser{
		tokens:  tokens,
		current: 0,
	}
}

// Parse parses the tokens into an AST
func (p *Parser) Parse() (prog *Program, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
				return
			}
			err = fmt.Errorf("parser panic: %v", r)
		}
	}()
	prog = &Program{
		Position:  Position{Line: 1, Column: 1},
		Assets:    make([]*AssetDecl, 0),
		Types:     make([]*TypeDecl, 0),
		Functions: make([]*FunctionDecl, 0),
	}

	for !p.isAtEnd() {
		// Skip newlines and indents at top level
		for p.check(TOKEN_NEWLINE) || p.check(TOKEN_INDENT) || p.check(TOKEN_DEDENT) {
			p.advance()
		}
		if p.isAtEnd() {
			break
		}

		if p.check(TOKEN_ASSET) {
			asset, err := p.parseAsset()
			if err != nil {
				return nil, err
			}
			prog.Assets = append(prog.Assets, asset)
		} else if p.check(TOKEN_TYPE) {
			typeDecl, err := p.parseTypeDecl()
			if err != nil {
				return nil, err
			}
			prog.Types = append(prog.Types, typeDecl)
		} else if p.check(TOKEN_FUNCTION) {
			fn, err := p.parseFunction()
			if err != nil {
				return nil, err
			}
			prog.Functions = append(prog.Functions, fn)
		} else if p.check(TOKEN_CONST) {
			c, err := p.parseConstDecl()
			if err != nil {
				return nil, err
			}
			prog.Consts = append(prog.Consts, c)
		} else if p.check(TOKEN_VAR) {
			g, err := p.parseGlobalVarDecl()
			if err != nil {
				return nil, err
			}
			prog.Globals = append(prog.Globals, g)
		} else if p.check(TOKEN_NEWLINE) {
			p.advance()
		} else {
			return nil, p.error(p.peek(), fmt.Sprintf("Expected asset, type, const, var, or function declaration, got %v", p.peek().Type))
		}
	}

	return prog, nil
}

// parseConstDecl parses: const NAME = expr
func (p *Parser) parseConstDecl() (*ConstDecl, error) {
	pos := p.position()
	p.consume(TOKEN_CONST, "Expected 'const'")
	nameTok := p.consume(TOKEN_IDENTIFIER, "Expected constant name after 'const'")
	p.consume(TOKEN_EQUAL, "Expected '=' after constant name")
	value := p.parseExpr()
	if !p.isAtEnd() && !p.check(TOKEN_NEWLINE) && !p.check(TOKEN_DEDENT) {
		return nil, p.error(p.peek(), "Unexpected token after constant value")
	}
	return &ConstDecl{Position: pos, Name: nameTok.Literal, Value: value}, nil
}

// parseGlobalVarDecl parses:
//   var name: type [= expr]
//   var name at 0xNNNN: type [= expr]
func (p *Parser) parseGlobalVarDecl() (*GlobalVarDecl, error) {
	pos := p.position()
	p.consume(TOKEN_VAR, "Expected 'var'")
	nameTok := p.consume(TOKEN_IDENTIFIER, "Expected variable name after 'var'")

	decl := &GlobalVarDecl{Position: pos, Name: nameTok.Literal}

	if p.check(TOKEN_AT) {
		p.advance()
		addrTok := p.consume(TOKEN_NUMBER, "Expected address literal after 'at'")
		addr, err := parseNumberLiteral(addrTok.Literal)
		if err != nil {
			return nil, p.error(addrTok, fmt.Sprintf("Invalid pin address: %v", err))
		}
		if addr < 0 || addr > 0xFFFF {
			return nil, p.error(addrTok, "Pin address out of 16-bit range")
		}
		decl.HasPin = true
		decl.PinAddr = uint16(addr)
	}

	p.consume(TOKEN_COLON, "Expected ':' and a type after the variable name")
	typeTok := p.consume(TOKEN_IDENTIFIER, "Expected type name after ':'")
	decl.TypeName = typeTok.Literal

	if p.check(TOKEN_LBRACKET) {
		p.advance()
		lenTok := p.consume(TOKEN_NUMBER, "Expected array length")
		n, err := parseNumberLiteral(lenTok.Literal)
		if err != nil || n <= 0 || n > 0x4000 {
			return nil, p.error(lenTok, "Array length must be between 1 and 16384")
		}
		decl.ArrayLen = int(n)
		p.consume(TOKEN_RBRACKET, "Expected ']' after array length")
	}

	if p.check(TOKEN_EQUAL) {
		p.advance()
		if p.check(TOKEN_LBRACKET) {
			// Array initializer: = [v0, v1, ...]
			p.advance()
			for !p.check(TOKEN_RBRACKET) && !p.isAtEnd() {
				decl.InitList = append(decl.InitList, p.parseExpr())
				if p.check(TOKEN_COMMA) {
					p.advance()
				}
			}
			p.consume(TOKEN_RBRACKET, "Expected ']' to close array initializer")
		} else {
			decl.Init = p.parseExpr()
		}
	}
	if !p.isAtEnd() && !p.check(TOKEN_NEWLINE) && !p.check(TOKEN_DEDENT) {
		return nil, p.error(p.peek(), "Unexpected token after variable declaration")
	}
	return decl, nil
}

func (p *Parser) parseAsset() (*AssetDecl, error) {
	pos := p.position()
	p.consume(TOKEN_ASSET, "Expected 'asset'")

	name := p.consume(TOKEN_IDENTIFIER, "Expected asset name").Literal
	p.consume(TOKEN_COLON, "Expected ':'")

	assetType := p.consume(TOKEN_IDENTIFIER, "Expected asset type").Literal
	if !isValidAssetType(assetType) {
		return nil, p.error(p.previous(), fmt.Sprintf("Invalid asset type: %s", assetType))
	}

	// image assets reference an external .cxasset file by path (hard split:
	// bitmap data lives in a side file, not inline).
	if assetType == "image" {
		pathTok := p.consume(TOKEN_STRING, "Expected a quoted .cxasset file path after 'image'")
		path := strings.Trim(pathTok.Literal, "\"")
		// consume trailing newline if present
		for p.check(TOKEN_NEWLINE) || p.check(TOKEN_INDENT) || p.check(TOKEN_DEDENT) {
			p.advance()
		}
		return &AssetDecl{Position: pos, Name: name, Type: "image", FilePath: path}, nil
	}

	// Encoding can be on same line or next line
	var encoding string
	if p.check(TOKEN_NEWLINE) {
		// Encoding is on next line
		p.advance()
		if p.check(TOKEN_INDENT) {
			p.advance()
		}
		encoding = p.consume(TOKEN_IDENTIFIER, "Expected encoding (b64, hex, or text)").Literal
	} else {
		// Encoding is on same line
		encoding = p.consume(TOKEN_IDENTIFIER, "Expected encoding (b64, hex, or text)").Literal
	}
	if !isValidAssetEncoding(encoding) {
		return nil, p.error(p.previous(), fmt.Sprintf("Invalid encoding: %s (expected b64, hex, or text)", encoding))
	}

	// Parse multi-line data
	// After encoding, we should be at the start of data (after newline/indent)
	var data strings.Builder
	// Skip to data lines
	if p.check(TOKEN_NEWLINE) {
		p.advance()
	}
	// Read data lines until dedent
	for !p.check(TOKEN_DEDENT) && !p.isAtEnd() {
		if p.check(TOKEN_NEWLINE) {
			p.advance()
			continue
		}
		if p.check(TOKEN_INDENT) {
			// Skip extra indents
			p.advance()
			continue
		}
		if p.check(TOKEN_STRING) {
			// Remove quotes
			lit := p.advance().Literal
			if len(lit) >= 2 && lit[0] == '"' && lit[len(lit)-1] == '"' {
				data.WriteString(lit[1 : len(lit)-1])
			} else {
				data.WriteString(lit)
			}
		} else if p.check(TOKEN_IDENTIFIER) || p.check(TOKEN_NUMBER) {
			lit := p.advance().Literal
			data.WriteString(lit)
			// Add space after tokens
			if !p.check(TOKEN_NEWLINE) && !p.check(TOKEN_DEDENT) {
				data.WriteString(" ")
			}
		} else {
			break
		}
	}
	if p.check(TOKEN_DEDENT) {
		p.advance()
	}

	return &AssetDecl{
		Position: pos,
		Name:     name,
		Type:     assetType,
		Encoding: encoding,
		Data:     strings.TrimSpace(data.String()),
	}, nil
}

func isValidAssetType(t string) bool {
	switch t {
	case "tiles8", "tiles16", "sprite", "tileset", "tilemap", "palette", "music", "sfx", "ambience", "gamedata", "blob", "image":
		return true
	default:
		return false
	}
}

func isValidAssetEncoding(enc string) bool {
	switch enc {
	case "hex", "b64", "text":
		return true
	default:
		return false
	}
}

func (p *Parser) parseTypeDecl() (*TypeDecl, error) {
	pos := p.position()
	p.consume(TOKEN_TYPE, "Expected 'type'")

	name := p.consume(TOKEN_IDENTIFIER, "Expected type name").Literal
	p.consume(TOKEN_EQUAL, "Expected '='")
	p.consume(TOKEN_STRUCT, "Expected 'struct'")

	// Parse struct fields
	fields := make([]*FieldDecl, 0)
	if p.check(TOKEN_NEWLINE) {
		p.advance()
		if p.check(TOKEN_INDENT) {
			p.advance()
			for !p.check(TOKEN_DEDENT) && !p.isAtEnd() {
				field, err := p.parseField()
				if err != nil {
					return nil, err
				}
				fields = append(fields, field)
				if !p.check(TOKEN_DEDENT) {
					p.consume(TOKEN_NEWLINE, "Expected newline after field")
				}
			}
			p.consume(TOKEN_DEDENT, "Expected dedent after struct fields")
		}
	}

	return &TypeDecl{
		Position: pos,
		Name:     name,
		Type: &StructType{
			Position: pos,
			Fields:   fields,
		},
	}, nil
}

func (p *Parser) parseField() (*FieldDecl, error) {
	pos := p.position()
	name := p.consume(TOKEN_IDENTIFIER, "Expected field name").Literal
	p.consume(TOKEN_COLON, "Expected ':'")
	typeExpr := p.parseTypeExpr()
	return &FieldDecl{
		Position: pos,
		Name:     name,
		Type:     typeExpr,
	}, nil
}

func (p *Parser) parseFunction() (*FunctionDecl, error) {
	pos := p.position()
	p.consume(TOKEN_FUNCTION, "Expected 'function'")

	name := p.consume(TOKEN_IDENTIFIER, "Expected function name").Literal

	// Parse parameters
	p.consume(TOKEN_LPAREN, "Expected '('")
	params := make([]*ParamDecl, 0)
	if !p.check(TOKEN_RPAREN) {
		for {
			param, err := p.parseParam()
			if err != nil {
				return nil, err
			}
			params = append(params, param)
			if !p.check(TOKEN_COMMA) {
				break
			}
			p.advance()
		}
	}
	p.consume(TOKEN_RPAREN, "Expected ')'")

	// Parse return type (optional)
	var returnType TypeExpr
	if p.check(TOKEN_ARROW) {
		p.advance()
		returnType = p.parseTypeExpr()
	}

	// Parse body
	body := make([]Stmt, 0)
	if p.check(TOKEN_NEWLINE) {
		p.advance()
		if p.check(TOKEN_INDENT) {
			p.advance()
			for !p.check(TOKEN_DEDENT) && !p.isAtEnd() {
				if p.check(TOKEN_NEWLINE) {
					p.advance()
					continue
				}
				stmt, err := p.parseStmt()
				if err != nil {
					return nil, err
				}
				if stmt != nil {
					body = append(body, stmt)
				}
			}
			if p.check(TOKEN_DEDENT) {
				p.advance()
			}
		}
	}

	return &FunctionDecl{
		Position:   pos,
		Name:       name,
		Params:     params,
		ReturnType: returnType,
		Body:       body,
	}, nil
}

func (p *Parser) parseParam() (*ParamDecl, error) {
	pos := p.position()
	name := p.consume(TOKEN_IDENTIFIER, "Expected parameter name").Literal
	p.consume(TOKEN_COLON, "Expected ':'")
	typeExpr := p.parseTypeExpr()
	return &ParamDecl{
		Position: pos,
		Name:     name,
		Type:     typeExpr,
	}, nil
}

func (p *Parser) parseTypeExpr() TypeExpr {
	pos := p.position()
	if p.check(TOKEN_STAR) {
		p.advance()
		base := p.parseTypeExpr()
		return &PointerType{
			Position: pos,
			Base:     base,
		}
	}
	if !p.check(TOKEN_IDENTIFIER) {
		panic(p.error(p.peek(), "Expected type name"))
	}
	name := p.advance().Literal
	return &NamedType{
		Position: pos,
		Name:     name,
	}
}

func (p *Parser) parseStmt() (Stmt, error) {
	if p.check(TOKEN_NEWLINE) {
		p.advance()
		return p.parseStmt()
	}

	pos := p.position()

	switch {
	case p.check(TOKEN_IDENTIFIER) && p.checkNext(TOKEN_ASSIGN):
		// Variable declaration: x := value
		name := p.advance().Literal
		p.advance() // consume :=
		value := p.parseExpr()
		if value == nil {
			return nil, p.error(p.peek(), "Expected expression")
		}
		return &VarDeclStmt{
			Position: pos,
			Name:     name,
			Value:    value,
		}, nil

	case p.check(TOKEN_IDENTIFIER) && p.checkNext(TOKEN_COLON):
		// Check if this is actually := (colon followed by =)
		// The lexer should have tokenized := as TOKEN_ASSIGN, but let's be safe
		name := p.advance().Literal
		colonTok := p.advance() // consume :
		if colonTok.Type != TOKEN_COLON {
			// This shouldn't happen if lexer is correct
			return nil, p.error(p.peek(), "Internal parser error: expected COLON")
		}
		// Check if next token is = (making it :=)
		if p.check(TOKEN_EQUAL) {
			// This is x: = value, which is invalid syntax
			// But maybe lexer didn't catch := properly?
			// For now, treat as error
			return nil, p.error(p.peek(), "Unexpected '=' after ':' - did you mean ':='?")
		}
		// Typed variable declaration: x: u8 = value
		typeExpr := p.parseTypeExpr()
		if typeExpr == nil {
			return nil, p.error(p.peek(), "Expected type name")
		}
		p.consume(TOKEN_EQUAL, "Expected '='")
		value := p.parseExpr()
		if value == nil {
			return nil, p.error(p.peek(), "Expected expression")
		}
		return &VarDeclStmt{
			Position: pos,
			Name:     name,
			Type:     typeExpr,
			Value:    value,
		}, nil

	case p.check(TOKEN_IF):
		return p.parseIfStmt()

	case p.check(TOKEN_WHILE):
		return p.parseWhileStmt()

	case p.check(TOKEN_FOR):
		return p.parseForStmt()

	case p.check(TOKEN_RETURN):
		p.advance()
		var value Expr
		if !p.check(TOKEN_NEWLINE) && !p.check(TOKEN_DEDENT) && !p.isAtEnd() {
			value = p.parseExpr()
		}
		return &ReturnStmt{
			Position: pos,
			Value:    value,
		}, nil

	default:
		// Try to parse as expression first
		expr := p.parseExpr()
		if expr == nil {
			return nil, p.error(p.peek(), "Expected statement")
		}
		// Check if it's actually an assignment (expression followed by =)
		if p.check(TOKEN_EQUAL) {
			// This is an assignment: target = value
			target := expr
			p.advance() // consume =
			value := p.parseExpr()
			if value == nil {
				return nil, p.error(p.peek(), "Expected expression")
			}
			return &AssignStmt{
				Position: pos,
				Target:   target,
				Value:    value,
			}, nil
		}
		// Expression statement
		return &ExprStmt{
			Position: pos,
			Expr:     expr,
		}, nil
	}
}

func (p *Parser) parseIfStmt() (*IfStmt, error) {
	pos := p.position()
	p.consume(TOKEN_IF, "Expected 'if'")
	condition := p.parseExpr()

	// Parse then block
	then := make([]Stmt, 0)
	if p.check(TOKEN_NEWLINE) {
		p.advance()
		if p.check(TOKEN_INDENT) {
			p.advance()
			for !p.check(TOKEN_DEDENT) && !p.isAtEnd() {
				if p.check(TOKEN_NEWLINE) {
					p.advance()
					continue
				}
				stmt, err := p.parseStmt()
				if err != nil {
					return nil, err
				}
				if stmt != nil {
					then = append(then, stmt)
				}
			}
			if p.check(TOKEN_DEDENT) {
				p.advance()
			}
		}
	}

	// Parse elseif/else clauses
	elseIf := make([]*ElseIfClause, 0)
	var elseBlock []Stmt

	for p.check(TOKEN_ELSEIF) {
		p.advance()
		cond := p.parseExpr()
		body := make([]Stmt, 0)
		if p.check(TOKEN_NEWLINE) {
			p.advance()
			if p.check(TOKEN_INDENT) {
				p.advance()
				for !p.check(TOKEN_DEDENT) && !p.isAtEnd() {
					stmt, err := p.parseStmt()
					if err != nil {
						return nil, err
					}
					body = append(body, stmt)
				}
				p.consume(TOKEN_DEDENT, "Expected dedent after elseif body")
			}
		}
		elseIf = append(elseIf, &ElseIfClause{
			Position:  p.position(),
			Condition: cond,
			Body:      body,
		})
	}

	if p.check(TOKEN_ELSE) {
		p.advance()
		elseBlock = make([]Stmt, 0)
		if p.check(TOKEN_NEWLINE) {
			p.advance()
			if p.check(TOKEN_INDENT) {
				p.advance()
				for !p.check(TOKEN_DEDENT) && !p.isAtEnd() {
					if p.check(TOKEN_NEWLINE) {
						p.advance()
						continue
					}
					stmt, err := p.parseStmt()
					if err != nil {
						return nil, err
					}
					if stmt != nil {
						elseBlock = append(elseBlock, stmt)
					}
				}
				if p.check(TOKEN_DEDENT) {
					p.advance()
				}
			}
		}
	}

	return &IfStmt{
		Position:  pos,
		Condition: condition,
		Then:      then,
		ElseIf:    elseIf,
		Else:      elseBlock,
	}, nil
}

func (p *Parser) parseWhileStmt() (*WhileStmt, error) {
	pos := p.position()
	p.consume(TOKEN_WHILE, "Expected 'while'")
	condition := p.parseExpr()
	if condition == nil {
		return nil, p.error(p.peek(), "Expected condition expression")
	}

	body := make([]Stmt, 0)
	if p.check(TOKEN_NEWLINE) {
		p.advance()
		if p.check(TOKEN_INDENT) {
			p.advance()
			for !p.check(TOKEN_DEDENT) && !p.isAtEnd() {
				if p.check(TOKEN_NEWLINE) {
					p.advance()
					continue
				}
				stmt, err := p.parseStmt()
				if err != nil {
					return nil, err
				}
				body = append(body, stmt)
			}
			if p.check(TOKEN_DEDENT) {
				p.advance()
			}
		}
	}

	return &WhileStmt{
		Position:  pos,
		Condition: condition,
		Body:      body,
	}, nil
}

func (p *Parser) parseForStmt() (*ForStmt, error) {
	pos := p.position()
	p.consume(TOKEN_FOR, "Expected 'for'")

	nameTok := p.consume(TOKEN_IDENTIFIER, "Expected loop variable name after 'for'")
	p.consume(TOKEN_EQUAL, "Expected '=' after loop variable (for i = start to end)")
	startExpr := p.parseExpr()
	p.consume(TOKEN_TO, "Expected 'to' in for loop (for i = start to end)")
	endExpr := p.parseExpr()

	var stepExpr Expr
	if p.check(TOKEN_STEP) {
		p.advance()
		stepExpr = p.parseExpr()
	}

	body := make([]Stmt, 0)
	if p.check(TOKEN_NEWLINE) {
		p.advance()
		if p.check(TOKEN_INDENT) {
			p.advance()
			for !p.check(TOKEN_DEDENT) && !p.isAtEnd() {
				if p.check(TOKEN_NEWLINE) {
					p.advance()
					continue
				}
				stmt, err := p.parseStmt()
				if err != nil {
					return nil, err
				}
				body = append(body, stmt)
			}
			if p.check(TOKEN_DEDENT) {
				p.advance()
			}
		}
	}

	return &ForStmt{
		Position: pos,
		VarName:  nameTok.Literal,
		Start:    startExpr,
		End:      endExpr,
		Step:     stepExpr,
		Body:     body,
	}, nil
}

func (p *Parser) parseExpr() Expr {
	return p.parseAssignment()
}

func (p *Parser) parseAssignment() Expr {
	// Assignment is handled in parseStmt, not in expression parsing
	// This function just parses expressions (no assignment)
	return p.parseOr()
}

func (p *Parser) parseOr() Expr {
	expr := p.parseAnd()
	for p.check(TOKEN_OR) {
		pos := p.position()
		op := p.advance().Type
		right := p.parseAnd()
		expr = &BinaryExpr{
			Position: pos,
			Op:       op,
			Left:     expr,
			Right:    right,
		}
	}
	return expr
}

func (p *Parser) parseAnd() Expr {
	expr := p.parseEquality()
	for p.check(TOKEN_AND) {
		pos := p.position()
		op := p.advance().Type
		right := p.parseEquality()
		expr = &BinaryExpr{
			Position: pos,
			Op:       op,
			Left:     expr,
			Right:    right,
		}
	}
	return expr
}

func (p *Parser) parseEquality() Expr {
	expr := p.parseComparison()
	for p.check(TOKEN_EQUAL_EQUAL) || p.check(TOKEN_BANG_EQUAL) {
		pos := p.position()
		op := p.advance().Type
		right := p.parseComparison()
		expr = &BinaryExpr{
			Position: pos,
			Op:       op,
			Left:     expr,
			Right:    right,
		}
	}
	return expr
}

func (p *Parser) parseComparison() Expr {
	expr := p.parseBitwise()
	for p.check(TOKEN_GREATER) || p.check(TOKEN_GREATER_EQUAL) ||
		p.check(TOKEN_LESS) || p.check(TOKEN_LESS_EQUAL) {
		pos := p.position()
		op := p.advance().Type
		right := p.parseBitwise()
		expr = &BinaryExpr{
			Position: pos,
			Op:       op,
			Left:     expr,
			Right:    right,
		}
	}
	return expr
}

func (p *Parser) parseBitwise() Expr {
	expr := p.parseShift()
	for p.check(TOKEN_AMPERSAND) || p.check(TOKEN_PIPE) || p.check(TOKEN_CARET) {
		pos := p.position()
		op := p.advance().Type
		right := p.parseShift()
		expr = &BinaryExpr{
			Position: pos,
			Op:       op,
			Left:     expr,
			Right:    right,
		}
	}
	return expr
}

func (p *Parser) parseShift() Expr {
	expr := p.parseTerm()
	for p.check(TOKEN_LSHIFT) || p.check(TOKEN_RSHIFT) {
		pos := p.position()
		op := p.advance().Type
		right := p.parseTerm()
		expr = &BinaryExpr{
			Position: pos,
			Op:       op,
			Left:     expr,
			Right:    right,
		}
	}
	return expr
}

func (p *Parser) parseTerm() Expr {
	expr := p.parseFactor()
	for p.check(TOKEN_PLUS) || p.check(TOKEN_MINUS) {
		pos := p.position()
		op := p.advance().Type
		right := p.parseFactor()
		expr = &BinaryExpr{
			Position: pos,
			Op:       op,
			Left:     expr,
			Right:    right,
		}
	}
	return expr
}

func (p *Parser) parseFactor() Expr {
	expr := p.parseUnary()
	for p.check(TOKEN_STAR) || p.check(TOKEN_SLASH) || p.check(TOKEN_PERCENT) {
		pos := p.position()
		op := p.advance().Type
		right := p.parseUnary()
		expr = &BinaryExpr{
			Position: pos,
			Op:       op,
			Left:     expr,
			Right:    right,
		}
	}
	return expr
}

func (p *Parser) parseUnary() Expr {
	if p.check(TOKEN_MINUS) || p.check(TOKEN_NOT) || p.check(TOKEN_TILDE) {
		pos := p.position()
		op := p.advance().Type
		operand := p.parseUnary()
		return &UnaryExpr{
			Position: pos,
			Op:       op,
			Operand:  operand,
		}
	}
	return p.parseCall()
}

func (p *Parser) parseCall() Expr {
	expr := p.parsePrimary()
	if expr == nil {
		return nil
	}
	for {
		if p.check(TOKEN_LPAREN) {
			pos := p.position()
			p.advance()
			args := make([]Expr, 0)
			if !p.check(TOKEN_RPAREN) {
				for {
					arg := p.parseExpr()
					if arg == nil {
						// Try to recover - maybe we hit a newline or dedent
						if p.check(TOKEN_NEWLINE) || p.check(TOKEN_DEDENT) {
							break
						}
						return nil // Error case
					}
					args = append(args, arg)
					if !p.check(TOKEN_COMMA) {
						break
					}
					p.advance()
				}
			}
			if !p.check(TOKEN_RPAREN) {
				// Missing closing paren - try to recover
				if p.check(TOKEN_NEWLINE) || p.check(TOKEN_DEDENT) {
					return nil
				}
			} else {
				p.advance()
			}
			expr = &CallExpr{
				Position: pos,
				Func:     expr,
				Args:     args,
			}
		} else if p.check(TOKEN_DOT) {
			pos := p.position()
			p.advance()
			if !p.check(TOKEN_IDENTIFIER) {
				return nil
			}
			member := p.advance().Literal
			expr = &MemberExpr{
				Position: pos,
				Object:   expr,
				Member:   member,
			}
		} else if p.check(TOKEN_LBRACKET) {
			pos := p.position()
			p.advance()
			index := p.parseExpr()
			p.consume(TOKEN_RBRACKET, "Expected ']' after array index")
			expr = &IndexExpr{
				Position: pos,
				Array:    expr,
				Index:    index,
			}
		} else {
			break
		}
	}
	return expr
}

func (p *Parser) parsePrimary() Expr {
	pos := p.position()

	switch {
	case p.check(TOKEN_TRUE):
		p.advance()
		return &BoolExpr{Position: pos, Value: true}
	case p.check(TOKEN_FALSE):
		p.advance()
		return &BoolExpr{Position: pos, Value: false}
	case p.check(TOKEN_NUMBER):
		tok := p.advance()
		if strings.Contains(tok.Literal, ".") {
			// Decimal literal => 8.8 fixed-point bits (charter D4).
			f, ferr := strconv.ParseFloat(tok.Literal, 64)
			if ferr != nil {
				panic(p.error(tok, "Invalid decimal literal"))
			}
			bits := int64(math.Round(f * 256.0))
			return &NumberExpr{
				Position: pos,
				Value:    uint64(uint16(bits)),
				IsFixed:  true,
			}
		}
		value, err := strconv.ParseUint(tok.Literal, 0, 64)
		if err != nil {
			// Try hex
			if strings.HasPrefix(tok.Literal, "0x") {
				value, _ = strconv.ParseUint(tok.Literal[2:], 16, 64)
			}
		}
		return &NumberExpr{
			Position: pos,
			Value:    value,
			IsHex:    strings.HasPrefix(tok.Literal, "0x"),
		}
	case p.check(TOKEN_STRING):
		tok := p.advance()
		value := tok.Literal
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		return &StringExpr{Position: pos, Value: value}
	case p.check(TOKEN_LPAREN):
		p.advance()
		expr := p.parseExpr()
		p.consume(TOKEN_RPAREN, "Expected ')'")
		return expr
	case p.check(TOKEN_IDENTIFIER):
		name := p.advance().Literal
		return &IdentExpr{Position: pos, Name: name}
	default:
		return nil // Return nil instead of panic - caller will handle error
	}
}

// Helper methods

func (p *Parser) advance() Token {
	if !p.isAtEnd() {
		p.current++
	}
	return p.previous()
}

func (p *Parser) peek() Token {
	if p.current >= len(p.tokens) {
		return Token{Type: TOKEN_EOF}
	}
	return p.tokens[p.current]
}

func (p *Parser) previous() Token {
	if p.current == 0 {
		return Token{Type: TOKEN_EOF}
	}
	return p.tokens[p.current-1]
}

func (p *Parser) check(tokenType TokenType) bool {
	if p.isAtEnd() {
		return false
	}
	return p.peek().Type == tokenType
}

func (p *Parser) checkNext(tokenType TokenType) bool {
	if p.current+1 >= len(p.tokens) {
		return false
	}
	return p.tokens[p.current+1].Type == tokenType
}

func (p *Parser) consume(tokenType TokenType, message string) Token {
	if p.check(tokenType) {
		return p.advance()
	}
	panic(p.error(p.peek(), message))
}

func (p *Parser) isAtEnd() bool {
	if p.current >= len(p.tokens) {
		return true
	}
	return p.tokens[p.current].Type == TOKEN_EOF
}

func (p *Parser) position() Position {
	if p.current == 0 {
		return Position{Line: 1, Column: 1}
	}
	tok := p.previous()
	return Position{Line: tok.Line, Column: tok.Column}
}

func (p *Parser) error(token Token, message string) error {
	return fmt.Errorf("parse error at line %d, column %d: %s", token.Line, token.Column, message)
}

// parseNumberLiteral converts a numeric token literal (decimal or 0x hex)
// to its integer value.
func parseNumberLiteral(lit string) (int64, error) {
	v, err := strconv.ParseUint(lit, 0, 64)
	if err != nil {
		return 0, err
	}
	return int64(v), nil
}
