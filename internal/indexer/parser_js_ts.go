package indexer

import (
	"fmt"
	"strings"
	"unsafe"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	treesitterjavascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	treesittertypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
	"github.com/tuffrabit/tuffman/internal/storage"
)

// jsParser holds the tree-sitter parser for JavaScript
var jsParser *treesitter.Parser

// tsParser holds the tree-sitter parser for TypeScript
var tsParser *treesitter.Parser

// tsxParser holds the tree-sitter parser for TSX (TypeScript with JSX)
var tsxParser *treesitter.Parser

func init() {
	// Initialize JavaScript parser
	jsParser = treesitter.NewParser()
	jsParser.SetLanguage(treesitter.NewLanguage(unsafe.Pointer(treesitterjavascript.Language())))

	// Initialize TypeScript parser
	tsParser = treesitter.NewParser()
	tsParser.SetLanguage(treesitter.NewLanguage(unsafe.Pointer(treesittertypescript.LanguageTypescript())))

	// Initialize TSX parser
	tsxParser = treesitter.NewParser()
	tsxParser.SetLanguage(treesitter.NewLanguage(unsafe.Pointer(treesittertypescript.LanguageTSX())))
}

// parseJavaScript parses JavaScript source code and extracts symbols
func (idx *Indexer) parseJavaScript(content []byte, path string) ([]*storage.Symbol, error) {
	return idx.parseJSWithParser(content, path, jsParser, LangJavaScript)
}

// parseTypeScript parses TypeScript source code and extracts symbols
func (idx *Indexer) parseTypeScript(content []byte, path string) ([]*storage.Symbol, error) {
	return idx.parseJSWithParser(content, path, tsParser, LangTypeScript)
}

// parseTSX parses TSX (TypeScript with JSX) source code and extracts symbols
func (idx *Indexer) parseTSX(content []byte, path string) ([]*storage.Symbol, error) {
	return idx.parseJSWithParser(content, path, tsxParser, LangTypeScript)
}

// parseJSWithParser parses JS/TS/TSX using the specified parser
func (idx *Indexer) parseJSWithParser(content []byte, path string, parser *treesitter.Parser, lang Language) ([]*storage.Symbol, error) {
	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil, nil
	}
	defer tree.Close()

	root := tree.RootNode()

	var symbols []*storage.Symbol

	walker := &jsASTWalker{
		content:  content,
		path:     path,
		symbols:  symbols,
		language: lang,
	}
	walker.walk(root)

	return walker.symbols, nil
}

// jsASTWalker walks the JavaScript/TypeScript AST and extracts symbols
type jsASTWalker struct {
	content  []byte
	path     string
	symbols  []*storage.Symbol
	language Language
}

// walk recursively walks the AST tree
func (w *jsASTWalker) walk(node *treesitter.Node) {
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
func (w *jsASTWalker) extractSymbol(node *treesitter.Node) *storage.Symbol {
	kind := node.Kind()

	switch kind {
	case "function_declaration":
		return w.extractFunction(node, "function")
	case "function":
		// Anonymous function in expression context
		return w.extractFunction(node, "function")
	case "arrow_function":
		return w.extractArrowFunction(node)
	case "method_definition":
		return w.extractMethod(node)
	case "class_declaration":
		return w.extractClass(node)
	case "interface_declaration":
		if w.language == LangTypeScript {
			return w.extractInterface(node)
		}
	case "type_alias_declaration":
		if w.language == LangTypeScript {
			return w.extractTypeAlias(node)
		}
	case "enum_declaration":
		if w.language == LangTypeScript {
			return w.extractEnum(node)
		}
	case "variable_declarator":
		// Only extract if it's assigned a function or class
		return w.extractVariableDeclarator(node)
	}

	return nil
}

// extractFunction extracts a function symbol
func (w *jsASTWalker) extractFunction(node *treesitter.Node, kind string) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		// Anonymous function - try to infer name from parent
		name := w.inferFunctionName(node)
		if name == "" {
			return nil
		}
		return w.createFunctionSymbol(node, name, kind)
	}

	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	return w.createFunctionSymbol(node, name, kind)
}

// extractArrowFunction extracts an arrow function symbol
func (w *jsASTWalker) extractArrowFunction(node *treesitter.Node) *storage.Symbol {
	name := w.inferFunctionName(node)
	if name == "" {
		return nil
	}

	signature := w.extractArrowFunctionSignature(node, name)

	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "function",
		Signature: signature,
		LineStart: startLine,
		LineEnd:   endLine,
	}
}

// extractMethod extracts a method symbol from a class
func (w *jsASTWalker) extractMethod(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	signature := w.extractMethodSignature(node, name)

	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "method",
		Signature: signature,
		LineStart: startLine,
		LineEnd:   endLine,
	}
}

// extractClass extracts a class symbol
func (w *jsASTWalker) extractClass(node *treesitter.Node) *storage.Symbol {
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

// extractInterface extracts a TypeScript interface symbol
func (w *jsASTWalker) extractInterface(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	signature := w.extractInterfaceSignature(node, name)

	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "interface",
		Signature: signature,
		LineStart: startLine,
		LineEnd:   endLine,
	}
}

// extractTypeAlias extracts a TypeScript type alias symbol
func (w *jsASTWalker) extractTypeAlias(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	// Get the type value
	var typeValue string
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil && child.Kind() != "type" && child.Kind() != "identifier" && child.Kind() != "=" {
			typeValue = w.nodeText(child)
			break
		}
	}

	if len(typeValue) > 100 {
		typeValue = typeValue[:97] + "..."
	}

	signature := fmt.Sprintf("type %s = %s", name, typeValue)

	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "type",
		Signature: signature,
		LineStart: startLine,
		LineEnd:   endLine,
	}
}

// extractEnum extracts a TypeScript enum symbol
func (w *jsASTWalker) extractEnum(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	signature := w.extractEnumSignature(node, name)

	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "enum",
		Signature: signature,
		LineStart: startLine,
		LineEnd:   endLine,
	}
}

// extractVariableDeclarator extracts a variable that's assigned a function or class
func (w *jsASTWalker) extractVariableDeclarator(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	// Get the value being assigned
	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		return nil
	}

	valueKind := valueNode.Kind()

	// Only extract if it's a function or class expression
	switch valueKind {
	case "function", "arrow_function", "class":
		// These are anonymous functions/classes assigned to variables
		kind := "function"
		if valueKind == "class" {
			kind = "class"
		}

		signature := w.extractVariableFunctionSignature(node, name, valueNode)

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

	return nil
}

// inferFunctionName tries to infer a function name from its context (e.g., variable assignment)
func (w *jsASTWalker) inferFunctionName(node *treesitter.Node) string {
	parent := node.Parent()
	if parent == nil {
		return ""
	}

	parentKind := parent.Kind()

	// Check if parent is a variable declarator
	if parentKind == "variable_declarator" {
		nameNode := parent.ChildByFieldName("name")
		if nameNode != nil {
			return w.nodeText(nameNode)
		}
	}

	// Check if parent is an assignment expression
	if parentKind == "assignment_expression" {
		leftNode := parent.ChildByFieldName("left")
		if leftNode != nil {
			return w.nodeText(leftNode)
		}
	}

	// Check if parent is a property in an object
	if parentKind == "pair" {
		keyNode := parent.ChildByFieldName("key")
		if keyNode != nil {
			return w.nodeText(keyNode)
		}
	}

	return ""
}

// createFunctionSymbol creates a function symbol with the given name
func (w *jsASTWalker) createFunctionSymbol(node *treesitter.Node, name, kind string) *storage.Symbol {
	signature := w.extractFunctionSignature(node, name)

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

// extractFunctionSignature extracts function parameters and return type
func (w *jsASTWalker) extractFunctionSignature(node *treesitter.Node, name string) string {
	paramsNode := node.ChildByFieldName("parameters")
	params := "()"
	if paramsNode != nil {
		params = w.nodeText(paramsNode)
	}

	// For TypeScript, check for return type annotation
	var returnType string
	if w.language == LangTypeScript {
		returnTypeNode := node.ChildByFieldName("return_type")
		if returnTypeNode != nil {
			returnType = w.nodeText(returnTypeNode)
		}
	}

	// Check for async
	isAsync := false
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil && child.Kind() == "async" {
			isAsync = true
			break
		}
	}

	prefix := "function"
	if isAsync {
		prefix = "async function"
	}

	signature := fmt.Sprintf("%s %s%s", prefix, name, params)
	if returnType != "" {
		signature += " " + returnType
	}

	return signature
}

// extractArrowFunctionSignature extracts arrow function signature
func (w *jsASTWalker) extractArrowFunctionSignature(node *treesitter.Node, name string) string {
	paramsNode := node.ChildByFieldName("parameters")
	params := "()"
	if paramsNode != nil {
		params = w.nodeText(paramsNode)
	}

	// Check for return type annotation (TypeScript)
	var returnType string
	if w.language == LangTypeScript {
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(uint(i))
			if child != nil && child.Kind() == "type_annotation" {
				returnType = w.nodeText(child)
				break
			}
		}
	}

	signature := fmt.Sprintf("const %s = %s =>", name, params)
	if returnType != "" {
		signature = fmt.Sprintf("const %s = %s %s =>", name, params, returnType)
	}

	return signature
}

// extractMethodSignature extracts method signature with async/generator info
func (w *jsASTWalker) extractMethodSignature(node *treesitter.Node, name string) string {
	paramsNode := node.ChildByFieldName("parameters")
	params := "()"
	if paramsNode != nil {
		params = w.nodeText(paramsNode)
	}

	// Check for async, static, get, set
	var modifiers []string
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil {
			kind := child.Kind()
			if kind == "async" || kind == "static" || kind == "get" || kind == "set" {
				modifiers = append(modifiers, kind)
			}
		}
	}

	prefix := ""
	if len(modifiers) > 0 {
		prefix = strings.Join(modifiers, " ") + " "
	}

	return fmt.Sprintf("%s%s%s", prefix, name, params)
}

// extractClassSignature extracts class signature with extends/implements
func (w *jsASTWalker) extractClassSignature(node *treesitter.Node, name string) string {
	var parts []string
	parts = append(parts, "class", name)

	// Check for extends
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil {
			kind := child.Kind()
			if kind == "extends_clause" {
				parts = append(parts, w.nodeText(child))
			} else if kind == "implements_clause" && w.language == LangTypeScript {
				parts = append(parts, w.nodeText(child))
			}
		}
	}

	return strings.Join(parts, " ") + " {}"
}

// extractInterfaceSignature extracts interface signature with extends
func (w *jsASTWalker) extractInterfaceSignature(node *treesitter.Node, name string) string {
	var parts []string
	parts = append(parts, "interface", name)

	// Check for extends
	extendsNode := node.ChildByFieldName("extends")
	if extendsNode != nil {
		parts = append(parts, w.nodeText(extendsNode))
	}

	return strings.Join(parts, " ") + " {}"
}

// extractEnumSignature extracts enum signature with its members
func (w *jsASTWalker) extractEnumSignature(node *treesitter.Node, name string) string {
	return fmt.Sprintf("enum %s {}", name)
}

// extractVariableFunctionSignature extracts signature for function assigned to variable
func (w *jsASTWalker) extractVariableFunctionSignature(node *treesitter.Node, name string, valueNode *treesitter.Node) string {
	kind := valueNode.Kind()

	if kind == "arrow_function" {
		return w.extractArrowFunctionSignature(valueNode, name)
	}

	// Regular function expression
	paramsNode := valueNode.ChildByFieldName("parameters")
	params := "()"
	if paramsNode != nil {
		params = w.nodeText(paramsNode)
	}

	return fmt.Sprintf("const %s = function%s", name, params)
}

// nodeText returns the text content of a node
func (w *jsASTWalker) nodeText(node *treesitter.Node) string {
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
