package indexer

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	treesittergo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	"github.com/tuffrabit/tuffman/internal/storage"
)

// goParser holds the tree-sitter parser for Go
var goParser *treesitter.Parser

func init() {
	goParser = treesitter.NewParser()
	goParser.SetLanguage(treesitter.NewLanguage(unsafe.Pointer(treesittergo.Language())))
}

// parseGo parses Go source code and extracts symbols using tree-sitter
func (idx *Indexer) parseGo(content []byte, absPath string) ([]*storage.Symbol, []*storage.Reference, error) {
	// Parse with tree-sitter - we get partial AST even on errors
	tree := goParser.Parse(content, nil)
	if tree == nil {
		// Only fail if we get absolutely nothing
		return nil, nil, nil
	}
	defer tree.Close()

	root := tree.RootNode()
	
	// Compute relative path for symbol IDs (consistent with storage)
	relPath, err := filepath.Rel(idx.config.ProjectRoot, absPath)
	if err != nil {
		relPath = absPath
	}
	relPath = filepath.ToSlash(relPath)
	
	// Walk the AST and extract symbols and references
	walker := &goASTWalker{
		content:   content,
		path:      relPath, // Use relative path for symbol IDs
		symbols:   nil,
		refs:      nil,
		symStack:  nil,
	}
	walker.walk(root)
	
	return walker.symbols, walker.refs, nil
}

// goASTWalker walks the Go AST and extracts symbols
type goASTWalker struct {
	content  []byte
	path     string
	symbols  []*storage.Symbol
	refs     []*storage.Reference
	symStack []*storage.Symbol // Stack of current symbol context
}

// walk recursively walks the AST tree
func (w *goASTWalker) walk(node *treesitter.Node) {
	if node == nil {
		return
	}

	// Check if this node represents a symbol
	if sym := w.extractSymbol(node); sym != nil {
		w.symbols = append(w.symbols, sym)
		// Push symbol onto stack for reference context
		w.symStack = append(w.symStack, sym)
		
		// Walk children with this symbol as context
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(uint(i))
			if child != nil {
				w.walk(child)
			}
		}
		
		// Pop symbol from stack
		w.symStack = w.symStack[:len(w.symStack)-1]
	} else {
		// Extract references if we're inside a symbol context
		if len(w.symStack) > 0 {
			w.extractReferences(node)
		}
		
		// Walk children without changing context
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(uint(i))
			if child != nil {
				w.walk(child)
			}
		}
	}
}

// extractSymbol extracts a symbol from a node if it represents one
func (w *goASTWalker) extractSymbol(node *treesitter.Node) *storage.Symbol {
	kind := node.Kind()
	
	switch kind {
	case "function_declaration":
		return w.extractFunction(node)
	case "method_declaration":
		return w.extractMethod(node)
	case "type_declaration":
		return w.extractTypeDeclaration(node)
	case "const_declaration":
		return w.extractConstDeclaration(node)
	case "var_declaration":
		return w.extractVarDeclaration(node)
	}
	
	return nil
}

// extractFunction extracts a function symbol
func (w *goASTWalker) extractFunction(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	
	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	signature := w.extractSignature(node)
	doc := w.extractDoc(node)
	
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "function",
		Signature: signature,
		Doc:       doc,
		LineStart: startLine,
		LineEnd:   endLine,
	}
}

// extractMethod extracts a method symbol
func (w *goASTWalker) extractMethod(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	
	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	receiver := w.extractReceiver(node)
	signature := w.extractSignature(node)
	doc := w.extractDoc(node)
	
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "method",
		Signature: signature,
		Doc:       doc,
		LineStart: startLine,
		LineEnd:   endLine,
		Receiver:  receiver,
	}
}

// extractTypeDeclaration extracts struct/interface/type declarations
func (w *goASTWalker) extractTypeDeclaration(node *treesitter.Node) *storage.Symbol {
	// type_declaration contains type_spec children
	var symbols []*storage.Symbol
	
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil && child.Kind() == "type_spec" {
			if sym := w.extractTypeSpec(child); sym != nil {
				symbols = append(symbols, sym)
			}
		}
	}
	
	// Return the first symbol (usually there's only one per declaration)
	if len(symbols) > 0 {
		return symbols[0]
	}
	return nil
}

// extractTypeSpec extracts a single type specification
func (w *goASTWalker) extractTypeSpec(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	
	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return nil
	}

	// Determine kind and extract detailed signature
	var kind string
	var signature string
	
	switch typeNode.Kind() {
	case "struct_type":
		kind = "struct"
		signature = w.extractStructSignature(typeNode, name)
	case "interface_type":
		kind = "interface"
		signature = w.extractInterfaceSignature(typeNode, name)
	default:
		kind = "type"
		signature = fmt.Sprintf("type %s %s", name, w.nodeText(typeNode))
	}

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

// extractStructSignature extracts struct fields as part of signature
func (w *goASTWalker) extractStructSignature(node *treesitter.Node, name string) string {
	var fields []string
	
	// Find field_declaration_list
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil && child.Kind() == "field_declaration_list" {
			// Iterate over field declarations
			for j := 0; j < int(child.ChildCount()); j++ {
				fieldDecl := child.Child(uint(j))
				if fieldDecl != nil && fieldDecl.Kind() == "field_declaration" {
					fieldStr := w.nodeText(fieldDecl)
					// Clean up the field string (remove extra whitespace)
					fieldStr = strings.Join(strings.Fields(fieldStr), " ")
					fields = append(fields, fieldStr)
				}
			}
		}
	}
	
	if len(fields) == 0 {
		return fmt.Sprintf("type %s struct {}", name)
	}
	
	return fmt.Sprintf("type %s struct {\n\t%s\n}", name, strings.Join(fields, "\n\t"))
}

// extractInterfaceSignature extracts interface methods as part of signature
func (w *goASTWalker) extractInterfaceSignature(node *treesitter.Node, name string) string {
	var methods []string
	
	// Find method_spec_list
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil && (child.Kind() == "method_spec_list" || child.Kind() == "interface_body") {
			// Iterate over method specs
			for j := 0; j < int(child.ChildCount()); j++ {
				methodSpec := child.Child(uint(j))
				if methodSpec != nil {
					kind := methodSpec.Kind()
					if kind == "method_spec" || kind == "interface_method" {
						methodStr := w.nodeText(methodSpec)
						methodStr = strings.Join(strings.Fields(methodStr), " ")
						methods = append(methods, methodStr)
					}
				}
			}
		}
	}
	
	if len(methods) == 0 {
		return fmt.Sprintf("type %s interface {}", name)
	}
	
	return fmt.Sprintf("type %s interface {\n\t%s\n}", name, strings.Join(methods, "\n\t"))
}

// extractConstDeclaration extracts const declarations
func (w *goASTWalker) extractConstDeclaration(node *treesitter.Node) *storage.Symbol {
	// const_declaration contains const_spec children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil && child.Kind() == "const_spec" {
			return w.extractConstSpec(child)
		}
	}
	return nil
}

// extractConstSpec extracts a single const specification
func (w *goASTWalker) extractConstSpec(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	
	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	typeNode := node.ChildByFieldName("type")
	valueNode := node.ChildByFieldName("value")
	
	var signatureParts []string
	if typeNode != nil {
		signatureParts = append(signatureParts, w.nodeText(typeNode))
	}
	if valueNode != nil {
		signatureParts = append(signatureParts, "= "+w.nodeText(valueNode))
	}
	
	signature := fmt.Sprintf("const %s %s", name, strings.Join(signatureParts, " "))
	signature = strings.TrimSpace(signature)
	
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "const",
		Signature: signature,
		LineStart: startLine,
		LineEnd:   endLine,
	}
}

// extractVarDeclaration extracts var declarations
func (w *goASTWalker) extractVarDeclaration(node *treesitter.Node) *storage.Symbol {
	// var_declaration contains var_spec children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil && child.Kind() == "var_spec" {
			return w.extractVarSpec(child)
		}
	}
	return nil
}

// extractVarSpec extracts a single var specification
func (w *goASTWalker) extractVarSpec(node *treesitter.Node) *storage.Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	
	name := w.nodeText(nameNode)
	if name == "" {
		return nil
	}

	typeNode := node.ChildByFieldName("type")
	valueNode := node.ChildByFieldName("value")
	
	var signatureParts []string
	if typeNode != nil {
		signatureParts = append(signatureParts, w.nodeText(typeNode))
	}
	if valueNode != nil {
		signatureParts = append(signatureParts, "= "+w.nodeText(valueNode))
	}
	
	signature := fmt.Sprintf("var %s %s", name, strings.Join(signatureParts, " "))
	signature = strings.TrimSpace(signature)
	
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", w.path, name, startLine),
		Name:      name,
		Kind:      "var",
		Signature: signature,
		LineStart: startLine,
		LineEnd:   endLine,
	}
}

// extractReceiver extracts the receiver type from a method declaration
func (w *goASTWalker) extractReceiver(node *treesitter.Node) string {
	receiverNode := node.ChildByFieldName("receiver")
	if receiverNode == nil {
		return ""
	}
	
	// receiver is a parameter_list containing a parameter
	for i := 0; i < int(receiverNode.ChildCount()); i++ {
		child := receiverNode.Child(uint(i))
		if child != nil && child.Kind() == "parameter_declaration" {
			// Get the type from the parameter
			typeNode := child.ChildByFieldName("type")
			if typeNode != nil {
				return w.nodeText(typeNode)
			}
		}
	}
	
	return ""
}

// extractSignature extracts the function/method signature
func (w *goASTWalker) extractSignature(node *treesitter.Node) string {
	paramsNode := node.ChildByFieldName("parameters")
	resultNode := node.ChildByFieldName("result")
	
	var parts []string
	
	if paramsNode != nil {
		parts = append(parts, w.nodeText(paramsNode))
	} else {
		parts = append(parts, "()")
	}
	
	if resultNode != nil {
		parts = append(parts, w.nodeText(resultNode))
	}
	
	return strings.Join(parts, " ")
}

// extractDoc extracts documentation comments preceding a node
func (w *goASTWalker) extractDoc(node *treesitter.Node) string {
	// Get the start position of the node
	startLine := int(node.StartPosition().Row)
	
	// Look for comment lines before this node
	var docs []string
	for i := startLine - 1; i >= 0; i-- {
		if i >= len(strings.Split(string(w.content), "\n")) {
			continue
		}
		lines := strings.Split(string(w.content), "\n")
		line := strings.TrimSpace(lines[i])
		
		if strings.HasPrefix(line, "//") {
			comment := strings.TrimSpace(strings.TrimPrefix(line, "//"))
			docs = append([]string{comment}, docs...)
		} else if line == "" {
			continue
		} else {
			break
		}
	}
	
	if len(docs) > 0 {
		return strings.Join(docs, "\n")
	}
	return ""
}

// nodeText returns the text content of a node
func (w *goASTWalker) nodeText(node *treesitter.Node) string {
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

// extractReferences extracts reference information from nodes
func (w *goASTWalker) extractReferences(node *treesitter.Node) {
	if len(w.symStack) == 0 {
		return
	}
	
	sourceID := w.symStack[len(w.symStack)-1].ID
	
	switch node.Kind() {
	case "call_expression":
		w.extractCallReference(node, sourceID)
	case "import_spec":
		w.extractImportReference(node, sourceID)
	}
}

// extractCallReference extracts a function call reference
func (w *goASTWalker) extractCallReference(node *treesitter.Node, sourceID string) {
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil {
		return
	}
	
	// Get the function name being called
	var targetName string
	
	switch funcNode.Kind() {
	case "identifier":
		// Simple function call: foo()
		targetName = w.nodeText(funcNode)
	case "selector_expression":
		// Method or package call: pkg.Func() or obj.Method()
		operand := funcNode.ChildByFieldName("operand")
		field := funcNode.ChildByFieldName("field")
		if field != nil {
			fieldName := w.nodeText(field)
			if operand != nil {
				operandName := w.nodeText(operand)
				targetName = operandName + "." + fieldName
			} else {
				targetName = fieldName
			}
		}
	case "call_expression":
		// Chained call: foo()()
		// Skip for now - complex chains
		return
	default:
		return
	}
	
	if targetName == "" {
		return
	}
	
	line := int(node.StartPosition().Row) + 1
	
	w.refs = append(w.refs, &storage.Reference{
		SourceID:   sourceID,
		TargetName: targetName,
		Kind:       "call",
		Line:       line,
	})
}

// extractImportReference extracts an import reference
func (w *goASTWalker) extractImportReference(node *treesitter.Node, sourceID string) {
	pathNode := node.ChildByFieldName("path")
	if pathNode == nil {
		return
	}
	
	importPath := w.nodeText(pathNode)
	// Remove quotes from import path
	importPath = strings.Trim(importPath, `"`)
	
	if importPath == "" {
		return
	}
	
	line := int(node.StartPosition().Row) + 1
	
	w.refs = append(w.refs, &storage.Reference{
		SourceID:   sourceID,
		TargetName: importPath,
		Kind:       "import",
		Line:       line,
	})
}
