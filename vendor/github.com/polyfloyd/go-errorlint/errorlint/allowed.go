package errorlint

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
)

var allowedErrors = []struct {
	err string
	fun string
}{
	// pkg/archive/tar
	{err: "io.EOF", fun: "(*archive/tar.Reader).Next"},
	{err: "io.EOF", fun: "(*archive/tar.Reader).Read"},
	// pkg/bufio
	{err: "io.EOF", fun: "(*bufio.Reader).Discard"},
	{err: "io.EOF", fun: "(*bufio.Reader).Peek"},
	{err: "io.EOF", fun: "(*bufio.Reader).Read"},
	{err: "io.EOF", fun: "(*bufio.Reader).ReadByte"},
	{err: "io.EOF", fun: "(*bufio.Reader).ReadBytes"},
	{err: "io.EOF", fun: "(*bufio.Reader).ReadLine"},
	{err: "io.EOF", fun: "(*bufio.Reader).ReadSlice"},
	{err: "io.EOF", fun: "(*bufio.Reader).ReadString"},
	{err: "io.EOF", fun: "(*bufio.Scanner).Scan"},
	// pkg/bytes
	{err: "io.EOF", fun: "(*bytes.Buffer).Read"},
	{err: "io.EOF", fun: "(*bytes.Buffer).ReadByte"},
	{err: "io.EOF", fun: "(*bytes.Buffer).ReadBytes"},
	{err: "io.EOF", fun: "(*bytes.Buffer).ReadRune"},
	{err: "io.EOF", fun: "(*bytes.Buffer).ReadString"},
	{err: "io.EOF", fun: "(*bytes.Reader).Read"},
	{err: "io.EOF", fun: "(*bytes.Reader).ReadAt"},
	{err: "io.EOF", fun: "(*bytes.Reader).ReadByte"},
	{err: "io.EOF", fun: "(*bytes.Reader).ReadRune"},
	{err: "io.EOF", fun: "(*bytes.Reader).ReadString"},
	// pkg/database/sql
	{err: "database/sql.ErrNoRows", fun: "(*database/sql.Row).Scan"},
	// pkg/debug/elf
	{err: "io.EOF", fun: "debug/elf.Open"},
	{err: "io.EOF", fun: "debug/elf.NewFile"},
	// pkg/io
	{err: "io.EOF", fun: "(io.ReadCloser).Read"},
	{err: "io.EOF", fun: "(io.Reader).Read"},
	{err: "io.EOF", fun: "(io.ReaderAt).ReadAt"},
	{err: "io.EOF", fun: "(*io.LimitedReader).Read"},
	{err: "io.EOF", fun: "(*io.SectionReader).Read"},
	{err: "io.EOF", fun: "(*io.SectionReader).ReadAt"},
	{err: "io.ErrClosedPipe", fun: "(*io.PipeWriter).Write"},
	{err: "io.ErrShortBuffer", fun: "io.ReadAtLeast"},
	{err: "io.ErrUnexpectedEOF", fun: "io.ReadAtLeast"},
	{err: "io.EOF", fun: "io.ReadFull"},
	{err: "io.ErrUnexpectedEOF", fun: "io.ReadFull"},
	// pkg/net/http
	{err: "net/http.ErrServerClosed", fun: "(*net/http.Server).ListenAndServe"},
	{err: "net/http.ErrServerClosed", fun: "(*net/http.Server).ListenAndServeTLS"},
	{err: "net/http.ErrServerClosed", fun: "(*net/http.Server).Serve"},
	{err: "net/http.ErrServerClosed", fun: "(*net/http.Server).ServeTLS"},
	{err: "net/http.ErrServerClosed", fun: "net/http.ListenAndServe"},
	{err: "net/http.ErrServerClosed", fun: "net/http.ListenAndServeTLS"},
	{err: "net/http.ErrServerClosed", fun: "net/http.Serve"},
	{err: "net/http.ErrServerClosed", fun: "net/http.ServeTLS"},
	// pkg/os
	{err: "io.EOF", fun: "(*os.File).Read"},
	{err: "io.EOF", fun: "(*os.File).ReadAt"},
	{err: "io.EOF", fun: "(*os.File).ReadDir"},
	{err: "io.EOF", fun: "(*os.File).Readdir"},
	{err: "io.EOF", fun: "(*os.File).Readdirnames"},
	// pkg/strings
	{err: "io.EOF", fun: "(*strings.Reader).Read"},
	{err: "io.EOF", fun: "(*strings.Reader).ReadAt"},
	{err: "io.EOF", fun: "(*strings.Reader).ReadByte"},
	{err: "io.EOF", fun: "(*strings.Reader).ReadRune"},
	// pkg/context
	{err: "context.DeadlineExceeded", fun: "(context.Context).Err"},
	{err: "context.Canceled", fun: "(context.Context).Err"},
}

var allowedErrorWildcards = []struct {
	err string
	fun string
}{
	// golang.org/x/sys/unix
	{err: "golang.org/x/sys/unix.E", fun: "golang.org/x/sys/unix."},
}

func isAllowedErrAndFunc(err, fun string) bool {
	for _, allow := range allowedErrorWildcards {
		if strings.HasPrefix(fun, allow.fun) && strings.HasPrefix(err, allow.err) {
			return true
		}
	}

	for _, allow := range allowedErrors {
		if allow.fun == fun && allow.err == err {
			return true
		}
	}
	return false
}

func isAllowedErrorComparison(pass *TypesInfoExt, binExpr *ast.BinaryExpr) bool {
	var errName string // `<package>.<name>`, e.g. `io.EOF`
	var callExprs []*ast.CallExpr

	// Figure out which half of the expression is the returned error and which
	// half is the presumed error declaration.
	for _, expr := range []ast.Expr{binExpr.X, binExpr.Y} {
		switch t := expr.(type) {
		case *ast.SelectorExpr:
			// A selector which we assume refers to a staticaly declared error
			// in a package.
			errName = selectorToString(pass, t)
		case *ast.Ident:
			// Identifier, most likely to be the `err` variable or whatever
			// produces it.
			callExprs = assigningCallExprs(pass, t, map[types.Object]bool{})
		case *ast.CallExpr:
			callExprs = append(callExprs, t)
		}
	}

	// Unimplemented or not sure, disallow the expression.
	if errName == "" || len(callExprs) == 0 {
		return false
	}

	// Map call expressions to the function name format of the allow list.
	functionNames := make([]string, len(callExprs))
	for i, callExpr := range callExprs {
		functionSelector, ok := callExpr.Fun.(*ast.SelectorExpr)
		if !ok {
			// If the function is not a selector it is not an Std function that is
			// allowed.
			return false
		}
		if sel, ok := pass.TypesInfo.Selections[functionSelector]; ok {
			functionNames[i] = fmt.Sprintf("(%s).%s", sel.Recv(), sel.Obj().Name())
		} else {
			// If there is no selection, assume it is a package.
			functionNames[i] = selectorToString(pass, callExpr.Fun.(*ast.SelectorExpr))
		}
	}

	// All assignments done must be allowed.
	for _, funcName := range functionNames {
		if !isAllowedErrAndFunc(errName, funcName) {
			return false
		}
	}
	return true
}

// assigningCallExprs finds all *ast.CallExpr nodes that are part of an
// *ast.AssignStmt that assign to the subject identifier.
func assigningCallExprs(pass *TypesInfoExt, subject *ast.Ident, visitedObjects map[types.Object]bool) []*ast.CallExpr {
	if subject.Obj == nil {
		return nil
	}

	// Find other identifiers that reference this same object.
	sobj := pass.TypesInfo.ObjectOf(subject)

	if visitedObjects[sobj] {
		return nil
	}
	visitedObjects[sobj] = true

	// Make sure to exclude the subject identifier as it will cause an infinite recursion and is
	// being used in a read operation anyway.
	identifiers := []*ast.Ident{}
	for _, ident := range pass.IdentifiersForObject[sobj] {
		if subject.Pos() != ident.Pos() {
			identifiers = append(identifiers, ident)
		}
	}

	// Find out whether the identifiers are part of an assignment statement.
	var callExprs []*ast.CallExpr
	for _, ident := range identifiers {
		parent := pass.NodeParent[ident]
		switch declT := parent.(type) {
		case *ast.AssignStmt:
			// The identifier is LHS of an assignment.
			assignment := declT

			assigningExpr := assignment.Rhs[0]
			// If the assignment is comprised of multiple expressions, find out
			// which RHS expression we should use by finding its index in the LHS.
			if len(assignment.Lhs) == len(assignment.Rhs) {
				for i, lhs := range assignment.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok && subject.Name == ident.Name {
						assigningExpr = assignment.Rhs[i]
						break
					}
				}
			}

			switch assignT := assigningExpr.(type) {
			case *ast.CallExpr:
				// Found the function call.
				callExprs = append(callExprs, assignT)
			case *ast.Ident:
				// Skip assignments here the RHS points to the same object as the subject.
				if assignT.Obj == subject.Obj {
					continue
				}
				// The subject was the result of assigning from another identifier.
				callExprs = append(callExprs, assigningCallExprs(pass, assignT, visitedObjects)...)
			default:
				// TODO: inconclusive?
			}
		}
	}
	return callExprs
}

func selectorToString(pass *TypesInfoExt, selExpr *ast.SelectorExpr) string {
	o := pass.TypesInfo.Uses[selExpr.Sel]
	return fmt.Sprintf("%s.%s", o.Pkg().Path(), o.Name())
}
