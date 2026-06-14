package corelx

// AST node types

// Node represents any AST node
type Node interface {
	Pos() Position
}

// Position represents a source position
type Position struct {
	Line   int
	Column int
}

// Program represents the root AST node
type Program struct {
	Position Position
	Assets   []*AssetDecl
	Types    []*TypeDecl
	Consts   []*ConstDecl
	Globals  []*GlobalVarDecl
	Functions []*FunctionDecl
}

// ConstDecl represents a top-level compile-time constant: const NAME = expr
type ConstDecl struct {
	Position Position
	Name     string
	Value    Expr
}

// GlobalVarDecl represents a top-level WRAM global:
//   var name: type [= expr]
//   var name at 0xNNNN: type [= expr]
type GlobalVarDecl struct {
	Position Position
	Name     string
	TypeName string
	ArrayLen int // 0 = scalar; N>0 = fixed-size array type[N]
	HasPin   bool
	PinAddr  uint16
	Init     Expr
}

// AssetDecl represents an asset declaration
type AssetDecl struct {
	Position Position
	Name     string
	Type     string // "tiles8" or "tiles16"
	Encoding string // "b64" or "hex"
	Data     string
}

// TypeDecl represents a type declaration
type TypeDecl struct {
	Position Position
	Name     string
	Type     TypeSpec
}

// TypeSpec represents a type specification
type TypeSpec interface {
	Node
	isTypeSpec()
}

// StructType represents a struct type
type StructType struct {
	Position Position
	Fields   []*FieldDecl
}

func (*StructType) isTypeSpec() {}

// FieldDecl represents a struct field declaration
type FieldDecl struct {
	Position Position
	Name     string
	Type     TypeExpr
}

// TypeExpr represents a type expression
type TypeExpr interface {
	Node
	isTypeExpr()
}

// NamedType represents a named type
type NamedType struct {
	Position Position
	Name     string
}

func (*NamedType) isTypeExpr() {}

// PointerType represents a pointer type
type PointerType struct {
	Position Position
	Base     TypeExpr
}

func (*PointerType) isTypeExpr() {}

// FunctionDecl represents a function declaration
type FunctionDecl struct {
	Position Position
	Name     string
	Params   []*ParamDecl
	ReturnType TypeExpr // nil if void
	Body     []Stmt
}

// ParamDecl represents a function parameter
type ParamDecl struct {
	Position Position
	Name     string
	Type     TypeExpr
}

// Stmt represents a statement
type Stmt interface {
	Node
	isStmt()
}

// VarDeclStmt represents a variable declaration
type VarDeclStmt struct {
	Position Position
	Name     string
	Type     TypeExpr // nil if inferred
	Value    Expr
}

func (*VarDeclStmt) isStmt() {}

// AssignStmt represents an assignment statement
type AssignStmt struct {
	Position Position
	Target   Expr
	Value    Expr
}

func (*AssignStmt) isStmt() {}

// IfStmt represents an if statement
type IfStmt struct {
	Position Position
	Condition Expr
	Then      []Stmt
	ElseIf    []*ElseIfClause
	Else      []Stmt
}

func (*IfStmt) isStmt() {}

// ElseIfClause represents an elseif clause
type ElseIfClause struct {
	Position Position
	Condition Expr
	Body      []Stmt
}

// WhileStmt represents a while statement
type WhileStmt struct {
	Position Position
	Condition Expr
	Body      []Stmt
}

func (*WhileStmt) isStmt() {}

// ForStmt represents a for statement
type ForStmt struct {
	Position Position
	VarName  string // loop variable (fresh local)
	Start    Expr   // initial value
	End      Expr   // inclusive limit
	Step     Expr   // nil = +1; must be a compile-time constant
	Body     []Stmt
}

func (*ForStmt) isStmt() {}

// ReturnStmt represents a return statement
type ReturnStmt struct {
	Position Position
	Value    Expr // nil if void return
}

func (*ReturnStmt) isStmt() {}

// ExprStmt represents an expression statement
type ExprStmt struct {
	Position Position
	Expr     Expr
}

func (*ExprStmt) isStmt() {}

// Expr represents an expression
type Expr interface {
	Node
	isExpr()
}

// BinaryExpr represents a binary expression
type BinaryExpr struct {
	Position Position
	Op       TokenType
	Left     Expr
	Right    Expr
}

func (*BinaryExpr) isExpr() {}

// UnaryExpr represents a unary expression
type UnaryExpr struct {
	Position Position
	Op       TokenType
	Operand  Expr
}

func (*UnaryExpr) isExpr() {}

// CallExpr represents a function call
type CallExpr struct {
	Position Position
	Func     Expr
	Args     []Expr
}

func (*CallExpr) isExpr() {}

// IdentExpr represents an identifier
type IdentExpr struct {
	Position Position
	Name     string
}

func (*IdentExpr) isExpr() {}

// NumberExpr represents a number literal
type NumberExpr struct {
	Position Position
	Value    uint64
	IsHex    bool
	IsFixed  bool // decimal literal (e.g. 3.6); Value holds the 8.8 bits
}

func (*NumberExpr) isExpr() {}

// StringExpr represents a string literal
type StringExpr struct {
	Position Position
	Value    string
}

func (*StringExpr) isExpr() {}

// BoolExpr represents a boolean literal
type BoolExpr struct {
	Position Position
	Value    bool
}

func (*BoolExpr) isExpr() {}

// MemberExpr represents a member access (struct.field)
type MemberExpr struct {
	Position Position
	Object   Expr
	Member   string
}

func (*MemberExpr) isExpr() {}

// IndexExpr represents an array/index access
type IndexExpr struct {
	Position Position
	Array    Expr
	Index    Expr
}

func (*IndexExpr) isExpr() {}

// Helper functions for Position
func (p *Program) Pos() Position { return p.Position }
func (c *ConstDecl) Pos() Position { return c.Position }
func (g *GlobalVarDecl) Pos() Position { return g.Position }
func (a *AssetDecl) Pos() Position { return a.Position }
func (t *TypeDecl) Pos() Position { return t.Position }
func (s *StructType) Pos() Position { return s.Position }
func (f *FieldDecl) Pos() Position { return f.Position }
func (n *NamedType) Pos() Position { return n.Position }
func (p *PointerType) Pos() Position { return p.Position }
func (f *FunctionDecl) Pos() Position { return f.Position }
func (p *ParamDecl) Pos() Position { return p.Position }
func (v *VarDeclStmt) Pos() Position { return v.Position }
func (a *AssignStmt) Pos() Position { return a.Position }
func (i *IfStmt) Pos() Position { return i.Position }
func (e *ElseIfClause) Pos() Position { return e.Position }
func (w *WhileStmt) Pos() Position { return w.Position }
func (f *ForStmt) Pos() Position { return f.Position }
func (r *ReturnStmt) Pos() Position { return r.Position }
func (e *ExprStmt) Pos() Position { return e.Position }
func (b *BinaryExpr) Pos() Position { return b.Position }
func (u *UnaryExpr) Pos() Position { return u.Position }
func (c *CallExpr) Pos() Position { return c.Position }
func (i *IdentExpr) Pos() Position { return i.Position }
func (n *NumberExpr) Pos() Position { return n.Position }
func (s *StringExpr) Pos() Position { return s.Position }
func (b *BoolExpr) Pos() Position { return b.Position }
func (m *MemberExpr) Pos() Position { return m.Position }
func (i *IndexExpr) Pos() Position { return i.Position }
