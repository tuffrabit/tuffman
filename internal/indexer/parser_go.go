package indexer

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/tuffrabit/tuffman/internal/storage"
)

// parseGo parses Go source code and extracts symbols using the Go standard library parser
func (idx *Indexer) parseGo(content []byte, path string) ([]*storage.Symbol, error) {
	fset := token.NewFileSet()
	
	// Parse with comments for doc extraction
	f, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		// Return empty symbols for unparseable files (don't fail entire indexing)
		return nil, nil
	}

	var symbols []*storage.Symbol

	// Extract package-level declarations
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if sym := idx.extractGoFunction(fset, d, content, path); sym != nil {
				symbols = append(symbols, sym)
			}

		case *ast.GenDecl:
			// Handle type declarations (structs, interfaces)
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						if sym := idx.extractGoType(fset, ts, d.Doc, content, path); sym != nil {
							symbols = append(symbols, sym)
						}
					}
				}
			}
		}
	}

	return symbols, nil
}

// extractGoFunction extracts a function or method symbol
func (idx *Indexer) extractGoFunction(fset *token.FileSet, decl *ast.FuncDecl, content []byte, path string) *storage.Symbol {
	name := decl.Name.Name
	if name == "" {
		return nil
	}

	// Determine kind and receiver
	var kind string
	var receiver string
	
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		kind = "method"
		receiver = idx.extractReceiverType(decl.Recv.List[0].Type)
	} else {
		kind = "function"
	}

	// Build signature
	signature := idx.buildSignature(decl)

	// Extract doc comment
	doc := ""
	if decl.Doc != nil {
		doc = decl.Doc.Text()
	}

	pos := fset.Position(decl.Pos())
	endPos := fset.Position(decl.End())

	sym := &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", path, name, pos.Line),
		Name:      name,
		Kind:      kind,
		Signature: signature,
		Doc:       doc,
		LineStart: pos.Line,
		LineEnd:   endPos.Line,
		Receiver:  receiver,
	}

	return sym
}

// extractGoType extracts a type declaration (struct, interface, etc.)
func (idx *Indexer) extractGoType(fset *token.FileSet, spec *ast.TypeSpec, doc *ast.CommentGroup, content []byte, path string) *storage.Symbol {
	name := spec.Name.Name
	if name == "" {
		return nil
	}

	// Determine kind based on type
	var kind string
	switch spec.Type.(type) {
	case *ast.StructType:
		kind = "struct"
	case *ast.InterfaceType:
		kind = "interface"
	default:
		kind = "type"
	}

	// Extract doc comment
	docText := ""
	if doc != nil {
		docText = doc.Text()
	}

	pos := fset.Position(spec.Pos())
	endPos := fset.Position(spec.End())

	sym := &storage.Symbol{
		ID:        fmt.Sprintf("%s#%s#%d", path, name, pos.Line),
		Name:      name,
		Kind:      kind,
		Doc:       docText,
		LineStart: pos.Line,
		LineEnd:   endPos.Line,
	}

	return sym
}

// extractReceiverType extracts the type name from a receiver expression
func (idx *Indexer) extractReceiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return idx.extractReceiverType(t.X)
	case *ast.IndexExpr:
		return idx.extractReceiverType(t.X)
	default:
		return ""
	}
}

// buildSignature builds a function/method signature string
func (idx *Indexer) buildSignature(decl *ast.FuncDecl) string {
	var sig strings.Builder

	// Parameters
	sig.WriteString("(")
	if decl.Type.Params != nil {
		params := []string{}
		for _, param := range decl.Type.Params.List {
			paramType := exprToString(param.Type)
			for _, name := range param.Names {
				params = append(params, fmt.Sprintf("%s %s", name.Name, paramType))
			}
			if len(param.Names) == 0 {
				params = append(params, paramType)
			}
		}
		sig.WriteString(strings.Join(params, ", "))
	}
	sig.WriteString(")")

	// Return values
	if decl.Type.Results != nil && len(decl.Type.Results.List) > 0 {
		sig.WriteString(" ")
		results := []string{}
		for _, result := range decl.Type.Results.List {
			resultType := exprToString(result.Type)
			if len(result.Names) > 0 {
				for _, name := range result.Names {
					results = append(results, fmt.Sprintf("%s %s", name.Name, resultType))
				}
			} else {
				results = append(results, resultType)
			}
		}
		if len(results) == 1 {
			sig.WriteString(results[0])
		} else {
			sig.WriteString("(")
			sig.WriteString(strings.Join(results, ", "))
			sig.WriteString(")")
		}
	}

	return sig.String()
}

// exprToString converts an AST expression to a string representation
func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.ArrayType:
		if e.Len == nil {
			return "[]" + exprToString(e.Elt)
		}
		return fmt.Sprintf("[%s]%s", exprToString(e.Len), exprToString(e.Elt))
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", exprToString(e.X), e.Sel.Name)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", exprToString(e.Key), exprToString(e.Value))
	case *ast.ChanType:
		switch e.Dir {
		case ast.SEND:
			return fmt.Sprintf("chan<- %s", exprToString(e.Value))
		case ast.RECV:
			return fmt.Sprintf("<-chan %s", exprToString(e.Value))
		default:
			return fmt.Sprintf("chan %s", exprToString(e.Value))
		}
	case *ast.FuncType:
		return "func(...)"
	case *ast.BasicLit:
		return e.Value
	case *ast.Ellipsis:
		return fmt.Sprintf("...%s", exprToString(e.Elt))
	default:
		return fmt.Sprintf("%T", expr)
	}
}

// extractGoDoc extracts the doc comment preceding a declaration (legacy, kept for compatibility)
func extractGoDoc(content []byte, lineStart int) string {
	lines := bytes.Split(content, []byte("\n"))
	var docs []string
	
	for i := lineStart - 2; i >= 0; i-- { // lineStart is 1-indexed
		if i < 0 || i >= len(lines) {
			break
		}
		line := strings.TrimSpace(string(lines[i]))
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

// getLineNumber returns the 1-indexed line number for a position in content
func getLineNumber(content []byte, pos int) int {
	line := 1
	for i := 0; i < pos && i < len(content); i++ {
		if content[i] == '\n' {
			line++
		}
	}
	return line
}

// scanLines is a helper to iterate over lines in content
func scanLines(content []byte, callback func(lineNum int, line []byte) bool) {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	lineNum := 1
	for scanner.Scan() {
		if !callback(lineNum, scanner.Bytes()) {
			break
		}
		lineNum++
	}
}
