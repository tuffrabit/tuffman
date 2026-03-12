package indexer

import (
	"fmt"
	"unsafe"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	treesitterpython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	"github.com/tuffrabit/tuffman/internal/storage"
)

// pythonParser holds the tree-sitter parser for Python
var pythonParser *treesitter.Parser

func init() {
	pythonParser = treesitter.NewParser()
	pythonParser.SetLanguage(treesitter.NewLanguage(unsafe.Pointer(treesitterpython.Language())))
}

// parsePython parses Python source code and extracts symbols using tree-sitter
func (idx *Indexer) parsePython(content []byte, path string) ([]*storage.Symbol, error) {
	tree := pythonParser.Parse(content, nil)
	if tree == nil {
		return nil, nil
	}
	defer tree.Close()

	root := tree.RootNode()

	var symbols []*storage.Symbol

	walker := &pythonASTWalker{
		content: content,
		path:    path,
		symbols: symbols,
	}
	walker.walk(root)

	return walker.symbols, nil
}

// pythonASTWalker walks the Python AST and extracts symbols
type pythonASTWalker struct {
	content []byte
	path    string
	symbols []*storage.Symbol
}

// walk recursively walks the AST tree
func (w *pythonASTWalker) walk(node *treesitter.Node) {
	if node == nil {
		return
	}

	// Check if this node represents a symbol
	if sym := w.extractSymbol(node); sym != nil {
		w.symbols = append(w.symbols, sym)
	}

	// Recursively walk children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil {
			w.walk(child)
		}
	}
}

// extractSymbol extracts a symbol from a node if it represents one
func (w *pythonASTWalker) extractSymbol(node *treesitter.Node) *storage.Symbol {
	kind := node.Kind()

	switch kind {
	case "function_definition":
		return w.extractFunction(node)
	case "class_definition":
		return w.extractClass(node)
	}

	return nil
}

// extractFunction extracts a function symbol (including async functions)
func (w *pythonASTWalker) extractFunction(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	// Check if this is a method (inside a class_definition)
	kind := "function"
	if w.isInsideClass(node) {
		kind = "method"
	}

	// Check for async
	isAsync := w.isAsyncFunction(node)

	signature := w.extractFunctionSignature(node, isAsync)

	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      kind,
		Signature: signature,
		LineStart: startLine,
		LineEnd:   endLine,
	}
}

// extractClass extracts a class symbol
func (w *pythonASTWalker) extractClass(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	signature := w.extractClassSignature(node, name)

	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "class",
		Signature: signature,
		LineStart: startLine,
		LineEnd:   endLine,
	}
}

// isInsideClass checks if a node is inside a class definition
func (w *pythonASTWalker) isInsideClass(node *treesitter.Node) bool {
	parent := node.Parent()
	for parent != nil {
		if parent.Kind() == "class_definition" {
			return true
		}
		parent = parent.Parent()
	}
	return false
}

// isAsyncFunction checks if a function definition has the 'async' keyword
func (w *pythonASTWalker) isAsyncFunction(node *treesitter.Node) bool {
	// The first child of a function_definition might be "async"
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil && child.Kind() == "async" {
			return true
		}
	}
	return false
}

// extractFunctionSignature extracts function parameters and return type
func (w *pythonASTWalker) extractFunctionSignature(node *treesitter.Node, isAsync bool) string {
	// Get function name
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}
	name := w.nodeText(nameNode)

	// Build signature prefix
	prefix := "def"
	if isAsync {
		prefix = "async def"
	}

	// Get parameters
	paramsNode := node.ChildByFieldName("parameters")
	params := "()"
	if paramsNode != nil {
		params = w.nodeText(paramsNode)
	}

	// Get return type annotation
	returnTypeNode := node.ChildByFieldName("return_type")
	var returnType string
	if returnTypeNode != nil {
		returnType = " -> " + w.nodeText(returnTypeNode)
	}

	signature := fmt.Sprintf("%s %s%s%s", prefix, name, params, returnType)
	return signature
}

// extractClassSignature extracts class signature with bases
func (w *pythonASTWalker) extractClassSignature(node *treesitter.Node, name string) string {
	// Get base classes
	basesNode := node.ChildByFieldName("superclasses")
	if basesNode != nil {
		bases := w.nodeText(basesNode)
		return fmt.Sprintf("class %s%s:", name, bases)
	}

	return fmt.Sprintf("class %s:", name)
}

// nodeText returns the text content of a node
func (w *pythonASTWalker) nodeText(node *treesitter.Node) string {
	if node == nil {
		return ""
	}
	start := node.StartByte()
	end := node.EndByte()
	contentLen := uint(len(w.content))
	if start >= contentLen || end > contentLen {
		return ""
	}
	return string(w.content[start:end])
}

// extractModuleLevelVariables extracts module-level variable assignments
func (w *pythonASTWalker) extractModuleLevelVariables(node *treesitter.Node) []*storage.Symbol {
	// Only process expression_statement or assignment nodes at module level
	if node.Kind() != "expression_statement" && node.Kind() != "assignment" {
		return nil
	}

	// Check if we're at module level (parent is module)
	if node.Parent() == nil || node.Parent().Kind() != "module" {
		return nil
	}

	var symbols []*storage.Symbol

	// Handle simple assignment: x = 5
	if node.Kind() == "assignment" {
		if sym := w.extractAssignment(node); sym != nil {
			symbols = append(symbols, sym)
		}
	}

	return symbols
}

// extractAssignment extracts a variable from an assignment
func (w *pythonASTWalker) extractAssignment(node *treesitter.Node) *storage.Symbol {
	// Get left side of assignment
	leftNode := node.ChildByFieldName("left")
	if leftNode == nil {
		return nil
	}

	// Only handle simple identifiers for now
	if leftNode.Kind() != "identifier" {
		return nil
	}

	name := w.nodeText(leftNode)
	if name == "" {
		return nil
	}

	// Get right side for signature
	rightNode := node.ChildByFieldName("right")
	var value string
	if rightNode != nil {
		value = w.nodeText(rightNode)
		// Truncate long values
		if len(value) > 50 {
			value = value[:47] + "..."
		}
	}

	signature := name
	if value != "" {
		signature = fmt.Sprintf("%s = %s", name, value)
	}

	startLine := int(node.StartPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "variable",
		Signature: signature,
		LineStart: startLine,
		LineEnd:   int(node.EndPosition().Row) + 1,
	}
}

// parsePythonWithVariables parses Python and also extracts module-level variables
func (idx *Indexer) parsePythonWithVariables(content []byte, path string) ([]*storage.Symbol, error) {
	tree := pythonParser.Parse(content, nil)
	if tree == nil {
		return nil, nil
	}
	defer tree.Close()

	root := tree.RootNode()

	var symbols []*storage.Symbol

	walker := &pythonASTWalker{
		content: content,
		path:    path,
		symbols: symbols,
	}

	// Walk the tree
	walker.walk(root)

	// Also extract module-level variables separately
	walker.extractVariablesFromModule(root)

	return walker.symbols, nil
}

// extractVariablesFromModule walks the module and extracts top-level variable assignments
func (w *pythonASTWalker) extractVariablesFromModule(node *treesitter.Node) {
	if node == nil {
		return
	}

	// Only process direct children of module
	if node.Kind() == "module" {
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(uint(i))
			if child == nil {
				continue
			}

			// Handle assignment nodes directly at module level
			if child.Kind() == "assignment" || child.Kind() == "augmented_assignment" {
				if sym := w.extractAssignment(child); sym != nil {
					w.symbols = append(w.symbols, sym)
				}
			}

			// Handle expression_statement containing assignment (e.g., x: int = 5)
			if child.Kind() == "expression_statement" {
				// Check if it contains an annotated assignment
				for j := 0; j < int(child.ChildCount()); j++ {
					inner := child.Child(uint(j))
					if inner != nil && inner.Kind() == "annotated_assignment" {
						if sym := w.extractAnnotatedAssignment(inner); sym != nil {
							w.symbols = append(w.symbols, sym)
						}
					}
				}
			}
		}
	}
}

// extractAnnotatedAssignment extracts a variable from an annotated assignment (x: int = 5)
func (w *pythonASTWalker) extractAnnotatedAssignment(node *treesitter.Node) *storage.Symbol {
	leftNode := node.ChildByFieldName("left")
	if leftNode == nil {
		return nil
	}

	if leftNode.Kind() != "identifier" {
		return nil
	}

	name := w.nodeText(leftNode)
	if name == "" {
		return nil
	}

	// Get type annotation
	typeNode := node.ChildByFieldName("type")
	var typeAnnot string
	if typeNode != nil {
		typeAnnot = w.nodeText(typeNode)
	}

	// Get value
	valueNode := node.ChildByFieldName("right")
	var value string
	if valueNode != nil {
		value = w.nodeText(valueNode)
		if len(value) > 50 {
			value = value[:47] + "..."
		}
	}

	var signature string
	if typeAnnot != "" && value != "" {
		signature = fmt.Sprintf("%s: %s = %s", name, typeAnnot, value)
	} else if typeAnnot != "" {
		signature = fmt.Sprintf("%s: %s", name, typeAnnot)
	} else if value != "" {
		signature = fmt.Sprintf("%s = %s", name, value)
	} else {
		signature = name
	}

	startLine := int(node.StartPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "variable",
		Signature: signature,
		LineStart: startLine,
		LineEnd:   int(node.EndPosition().Row) + 1,
	}
}
