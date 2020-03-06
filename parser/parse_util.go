package parser

import (
	"fmt"

	"github.com/lighttiger2505/sqls/ast"
	"github.com/lighttiger2505/sqls/ast/astutil"
	"github.com/lighttiger2505/sqls/token"
	"golang.org/x/xerrors"
)

type TableInfo struct {
	DatabaseSchema string
	Name           string
	Alias          string
}

type SubQueryInfo struct {
	Name  string
	Views []*SubQueryView
}

type SubQueryView struct {
	Table   *TableInfo
	Columns []string
}

var statementTypeMatcher = astutil.NodeMatcher{
	NodeTypeMatcherFunc: func(node interface{}) bool {
		if _, ok := node.(*ast.Statement); ok {
			return true
		}
		return false
	},
}

func extractFocusedStatement(parsed ast.TokenList, pos token.Pos) (ast.TokenList, error) {
	nodeWalker := NewNodeWalker(parsed, pos)
	if !nodeWalker.CurNodeIs(statementTypeMatcher) {
		return nil, xerrors.Errorf("Not found statement, Node: %q, Position: (%d, %d)", parsed.String(), pos.Line, pos.Col)
	}
	stmt := nodeWalker.CurNodeTopMatched(statementTypeMatcher).(ast.TokenList)
	return stmt, nil
}

var parenthesisTypeMatcher = astutil.NodeMatcher{
	NodeTypeMatcherFunc: func(node interface{}) bool {
		if _, ok := node.(*ast.Parenthesis); ok {
			return true
		}
		return false
	},
}
var selectMatcher = astutil.NodeMatcher{
	ExpectKeyword: []string{
		"SELECT",
	},
}

func encloseIsSubQuery(stmt ast.TokenList, pos token.Pos) bool {
	nodeWalker := NewNodeWalker(stmt, pos)
	if !nodeWalker.CurNodeIs(parenthesisTypeMatcher) {
		return false
	}
	parenthesis := nodeWalker.CurNodeButtomMatched(parenthesisTypeMatcher)
	tokenList, ok := parenthesis.(ast.TokenList)
	if !ok {
		return false
	}
	reader := astutil.NewNodeReader(tokenList)
	if !reader.NextNode(false) {
		return false
	}
	if !reader.NextNode(false) {
		return false
	}
	if !reader.CurNodeIs(selectMatcher) {
		return false
	}
	return true
}

func extractFocusedSubQuery(stmt ast.TokenList, pos token.Pos) ast.TokenList {
	nodeWalker := NewNodeWalker(stmt, pos)
	if !nodeWalker.CurNodeIs(parenthesisTypeMatcher) {
		return nil
	}
	parenthesis := nodeWalker.CurNodeButtomMatched(parenthesisTypeMatcher)
	return parenthesis.(ast.TokenList)
}

func extractFocusedSubQueryWithAlias(stmt ast.TokenList, pos token.Pos) ast.TokenList {
	nodeWalker := NewNodeWalker(stmt, pos)
	if !nodeWalker.CurNodeIs(parenthesisTypeMatcher) {
		return nil
	}
	parenthesis := nodeWalker.CurNodeButtomMatched(parenthesisTypeMatcher)
	return parenthesis.(ast.TokenList)
}

func ExtractSubQueryView(stmt ast.TokenList) (*SubQueryInfo, error) {
	p, ok := stmt.(*ast.Parenthesis)
	if !ok {
		return nil, xerrors.Errorf("Is not sub query, query: %q, type: %T", stmt, stmt)
	}

	// extract select identifiers
	sbIdents := []string{}
	toks := p.Inner().GetTokens()
	switch v := toks[2].(type) {
	case ast.TokenList:
		identifiers := filterTokenList(astutil.NewNodeReader(v), identifierMatcher)
		for _, ident := range identifiers.GetTokens() {
			res, err := parseSubQueryColumns(ident)
			if err != nil {
				return nil, err
			}
			sbIdents = append(sbIdents, res...)
		}
	case *ast.Identifer:
		res, err := parseSubQueryColumns(v)
		if err != nil {
			return nil, err
		}
		sbIdents = append(sbIdents, res...)
	default:
		return nil, xerrors.Errorf("failed read the TokenList of select, query: %q, type: %T", toks[2], toks[2])
	}

	// extract table identifiers
	fromJoinExpr := filterTokenList(astutil.NewNodeReader(p.Inner()), fromJoinMatcher)
	fromIdentifiers := filterTokenList(astutil.NewNodeReader(fromJoinExpr), identifierMatcher)
	sbTables := []*TableInfo{}
	for _, ident := range fromIdentifiers.GetTokens() {
		res, err := parseTableInfo(ident)
		if err != nil {
			return nil, err
		}
		sbTables = append(sbTables, res...)
	}

	return &SubQueryInfo{
		Views: []*SubQueryView{
			&SubQueryView{
				Table:   sbTables[0],
				Columns: sbIdents,
			},
		},
	}, nil
}

func ExtractTable(parsed ast.TokenList, pos token.Pos) ([]*TableInfo, error) {
	stmt, err := extractFocusedStatement(parsed, pos)
	if err != nil {
		return nil, err
	}
	list := stmt
	if encloseIsSubQuery(stmt, pos) {
		list = extractFocusedSubQuery(stmt, pos)
	}
	fromJoinExpr := filterTokenList(astutil.NewNodeReader(list), fromJoinMatcher)
	identifiers := filterTokenList(astutil.NewNodeReader(fromJoinExpr), identifierMatcher)

	res := []*TableInfo{}
	for _, ident := range identifiers.GetTokens() {
		infos, err := parseTableInfo(ident)
		if err != nil {
			return nil, err
		}
		res = append(res, infos...)
	}
	return res, nil
}

var fromJoinMatcher = astutil.NodeMatcher{
	NodeTypeMatcherFunc: func(node interface{}) bool {
		if _, ok := node.(*ast.FromClause); ok {
			return true
		}
		if _, ok := node.(*ast.JoinClause); ok {
			return true
		}
		return false
	},
}

var identifierMatcher = astutil.NodeMatcher{
	NodeTypeMatcherFunc: func(node interface{}) bool {
		if _, ok := node.(*ast.Identifer); ok {
			return true
		}
		if _, ok := node.(*ast.IdentiferList); ok {
			return true
		}
		if _, ok := node.(*ast.MemberIdentifer); ok {
			return true
		}
		if _, ok := node.(*ast.Aliased); ok {
			return true
		}
		return false
	},
}

func filterTokenList(reader *astutil.NodeReader, matcher astutil.NodeMatcher) ast.TokenList {
	var res []ast.Node
	for reader.NextNode(false) {
		if reader.CurNodeIs(matcher) {
			res = append(res, reader.CurNode)
		} else if list, ok := reader.CurNode.(ast.TokenList); ok {
			newReader := astutil.NewNodeReader(list)
			res = append(res, filterTokenList(newReader, matcher).GetTokens()...)
		}
	}
	return &ast.Statement{Toks: res}
}

func filterTokens(toks []ast.Node, matcher astutil.NodeMatcher) []ast.Node {
	res := []ast.Node{}
	for _, tok := range toks {
		if matcher.IsMatch(tok) {
			res = append(res, tok)
		}
	}
	return res
}

func parseTableInfo(idents ast.Node) ([]*TableInfo, error) {
	res := []*TableInfo{}
	switch v := idents.(type) {
	case *ast.Identifer:
		ti := &TableInfo{Name: v.String()}
		res = append(res, ti)
	case *ast.IdentiferList:
		res = append(res, identifierListToTableInfo(v)...)
	case *ast.MemberIdentifer:
		if v.Parent != nil {
			ti := &TableInfo{
				DatabaseSchema: v.Parent.String(),
				Name:           v.Child.String(),
			}
			res = append(res, ti)
		}
	case *ast.Aliased:
		res = append(res, aliasedToTableInfo(v))
	default:
		return nil, xerrors.Errorf("unknown node type %T", v)
	}
	return res, nil
}

func identifierListToTableInfo(il *ast.IdentiferList) []*TableInfo {
	tis := []*TableInfo{}
	idents := filterTokens(il.GetTokens(), identifierMatcher)
	for _, ident := range idents {
		switch v := ident.(type) {
		case *ast.Identifer:
			ti := &TableInfo{
				Name: v.String(),
			}
			tis = append(tis, ti)
		case *ast.MemberIdentifer:
			ti := &TableInfo{
				DatabaseSchema: v.Parent.String(),
				Name:           v.Child.String(),
			}
			tis = append(tis, ti)
		default:
			// FIXME add error tracking
			panic(fmt.Sprintf("unknown node type %T", v))
		}
	}
	return tis
}

func aliasedToTableInfo(aliased *ast.Aliased) *TableInfo {
	ti := &TableInfo{}
	// fetch table schema and name
	switch v := aliased.RealName.(type) {
	case *ast.Identifer:
		ti.Name = v.String()
	case *ast.MemberIdentifer:
		ti.DatabaseSchema = v.Parent.String()
		ti.Name = v.Child.String()
	case *ast.Parenthesis:
		// Through
	default:
		// FIXME add error tracking
		panic(fmt.Sprintf("unknown node type, want Identifer or MemberIdentifier, got %T", v))
	}

	// fetch table aliased name
	switch v := aliased.AliasedName.(type) {
	case *ast.Identifer:
		ti.Alias = v.String()
	default:
		// FIXME add error tracking
		panic(fmt.Sprintf("unknown node type, want Identifer, got %T", v))
	}
	return ti
}

func parseSubQueryColumns(idents ast.Node) ([]string, error) {
	res := []string{}
	switch v := idents.(type) {
	case *ast.Identifer:
		res = append(res, v.String())
	case *ast.IdentiferList:
		res = append(res, identifierListToSubQueryColumn(v)...)
	case *ast.MemberIdentifer:
		res = append(res, v.Child.String())
	case *ast.Aliased:
		res = append(res, aliasedToSubQueryColumn(v))
	default:
		return nil, xerrors.Errorf("unknown node type %T", v)
	}
	return res, nil
}

func identifierListToSubQueryColumn(il *ast.IdentiferList) []string {
	res := []string{}
	idents := filterTokens(il.GetTokens(), identifierMatcher)
	for _, ident := range idents {
		switch v := ident.(type) {
		case *ast.Identifer:
			res = append(res, v.String())
		case *ast.MemberIdentifer:
			res = append(res, v.Child.String())
		default:
			// FIXME add error tracking
			panic(fmt.Sprintf("unknown node type %T", v))
		}
	}
	return res
}

func aliasedToSubQueryColumn(aliased *ast.Aliased) string {
	// fetch table schema and name
	switch v := aliased.RealName.(type) {
	case *ast.Identifer:
		return v.String()
	case *ast.MemberIdentifer:
		return v.Child.String()
	default:
		// FIXME add error tracking
		panic(fmt.Sprintf("unknown node type, want Identifer or MemberIdentifier, got %T", v))
	}

	// fetch table aliased name
	switch v := aliased.AliasedName.(type) {
	case *ast.Identifer:
		return v.String()
	default:
		// FIXME add error tracking
		panic(fmt.Sprintf("unknown node type, want Identifer, got %T", v))
	}
}

type NodeWalker struct {
	Paths   []*astutil.NodeReader
	CurPath *astutil.NodeReader
	Index   int
}

func astPaths(reader *astutil.NodeReader, pos token.Pos) []*astutil.NodeReader {
	paths := []*astutil.NodeReader{}
	for reader.NextNode(false) {
		if reader.CurNodeEncloseIs(pos) {
			paths = append(paths, reader)
			if list, ok := reader.CurNode.(ast.TokenList); ok {
				newReader := astutil.NewNodeReader(list)
				return append(paths, astPaths(newReader, pos)...)
			} else {
				return paths
			}
		}
	}
	return paths
}

func NewNodeWalker(root ast.TokenList, pos token.Pos) *NodeWalker {
	return &NodeWalker{
		Paths: astPaths(astutil.NewNodeReader(root), pos),
	}
}

func (nw *NodeWalker) CurNodeIs(matcher astutil.NodeMatcher) bool {
	for _, reader := range nw.Paths {
		if reader.CurNodeIs(matcher) {
			return true
		}
	}
	return false
}

func (nw *NodeWalker) CurNodeMatches(matcher astutil.NodeMatcher) []ast.Node {
	matches := []ast.Node{}
	for _, reader := range nw.Paths {
		if reader.CurNodeIs(matcher) {
			matches = append(matches, reader.CurNode)
		}
	}
	return matches
}

func (nw *NodeWalker) CurNodeTopMatched(matcher astutil.NodeMatcher) ast.Node {
	matches := nw.CurNodeMatches(matcher)
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

func (nw *NodeWalker) CurNodeButtomMatched(matcher astutil.NodeMatcher) ast.Node {
	matches := nw.CurNodeMatches(matcher)
	if len(matches) == 0 {
		return nil
	}
	return matches[len(matches)-1]
}

func (nw *NodeWalker) PrevNodesIs(ignoreWitespace bool, matcher astutil.NodeMatcher) bool {
	for _, reader := range nw.Paths {
		if reader.PrevNodeIs(ignoreWitespace, matcher) {
			return true
		}
	}
	return false
}
