package parser

import (
	"context"
	"testing"

	"github.com/genjidb/genji/sql/planner"
	"github.com/genjidb/genji/sql/query/expr"
	"github.com/genjidb/genji/sql/scanner"
	"github.com/stretchr/testify/require"
)

func TestParserSelect(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		expected *planner.Tree
		mustFail bool
	}{
		{"NoTable", "SELECT 1",
			planner.NewTree(planner.NewProjectionNode(nil,
				[]planner.ProjectedField{
					planner.ProjectedExpr{Expr: expr.IntegerValue(1), ExprName: "1"},
				}, "")),
			false,
		},
		{"NoCond", "SELECT * FROM test",
			planner.NewTree(
				planner.NewProjectionNode(
					planner.NewTableInputNode("test"),
					[]planner.ProjectedField{planner.Wildcard{}},
					"test",
				)),
			false},
		{"WithFields", "SELECT a, b FROM test",
			planner.NewTree(
				planner.NewProjectionNode(
					planner.NewTableInputNode("test"),
					[]planner.ProjectedField{planner.ProjectedExpr{Expr: expr.FieldSelector(parsePath(t, "a")), ExprName: "a"}, planner.ProjectedExpr{Expr: expr.FieldSelector(parsePath(t, "b")), ExprName: "b"}},
					"test",
				)),
			false},
		{"WithFieldsWithQuotes", "SELECT `long \"path\"` FROM test",
			planner.NewTree(
				planner.NewProjectionNode(
					planner.NewTableInputNode("test"),
					[]planner.ProjectedField{planner.ProjectedExpr{Expr: expr.FieldSelector(parsePath(t, "`long \"path\"`")), ExprName: "long \"path\""}},
					"test",
				)),
			false},
		{"WithAlias", "SELECT a AS A, b FROM test",
			planner.NewTree(
				planner.NewProjectionNode(
					planner.NewTableInputNode("test"),
					[]planner.ProjectedField{planner.ProjectedExpr{Expr: expr.FieldSelector(parsePath(t, "a")), ExprName: "A"}, planner.ProjectedExpr{Expr: expr.FieldSelector(parsePath(t, "b")), ExprName: "b"}},
					"test",
				)),
			false},
		{"WithFields and wildcard", "SELECT a, b, * FROM test",
			planner.NewTree(
				planner.NewProjectionNode(
					planner.NewTableInputNode("test"),
					[]planner.ProjectedField{planner.ProjectedExpr{Expr: expr.FieldSelector(parsePath(t, "a")), ExprName: "a"}, planner.ProjectedExpr{Expr: expr.FieldSelector(parsePath(t, "b")), ExprName: "b"}, planner.Wildcard{}},
					"test",
				)),
			false},
		{"WithExpr", "SELECT a    > 1 FROM test",
			planner.NewTree(
				planner.NewProjectionNode(
					planner.NewTableInputNode("test"),
					[]planner.ProjectedField{planner.ProjectedExpr{Expr: expr.Gt(expr.FieldSelector(parsePath(t, "a")), expr.IntegerValue(1)), ExprName: "a    > 1"}},
					"test",
				)),
			false},
		{"WithCond", "SELECT * FROM test WHERE age = 10",
			planner.NewTree(
				planner.NewProjectionNode(
					planner.NewSelectionNode(
						planner.NewTableInputNode("test"),
						expr.Eq(expr.FieldSelector(parsePath(t, "age")), expr.IntegerValue(10)),
					),
					[]planner.ProjectedField{planner.Wildcard{}},
					"test",
				)),
			false},
		{"WithGroupBy", "SELECT * FROM test WHERE age = 10 GROUP BY a.b.c",
			planner.NewTree(
				planner.NewProjectionNode(
					planner.NewGroupingNode(
						planner.NewSelectionNode(
							planner.NewTableInputNode("test"),
							expr.Eq(expr.FieldSelector(parsePath(t, "age")), expr.IntegerValue(10)),
						),
						expr.FieldSelector(parsePath(t, "a.b.c")),
					),
					[]planner.ProjectedField{planner.Wildcard{}},
					"test",
				)),
			false},
		{"WithOrderBy", "SELECT * FROM test WHERE age = 10 ORDER BY a.b.c",
			planner.NewTree(
				planner.NewSortNode(
					planner.NewProjectionNode(
						planner.NewSelectionNode(
							planner.NewTableInputNode("test"),
							expr.Eq(expr.FieldSelector(parsePath(t, "age")), expr.IntegerValue(10)),
						),
						[]planner.ProjectedField{planner.Wildcard{}},
						"test",
					),
					expr.FieldSelector(parsePath(t, "a.b.c")),
					scanner.ASC,
				)),
			false},
		{"WithOrderBy ASC", "SELECT * FROM test WHERE age = 10 ORDER BY a.b.c ASC",
			planner.NewTree(
				planner.NewSortNode(
					planner.NewProjectionNode(
						planner.NewSelectionNode(
							planner.NewTableInputNode("test"),
							expr.Eq(expr.FieldSelector(parsePath(t, "age")), expr.IntegerValue(10)),
						),
						[]planner.ProjectedField{planner.Wildcard{}},
						"test",
					),
					expr.FieldSelector(parsePath(t, "a.b.c")),
					scanner.ASC,
				)),
			false},
		{"WithOrderBy DESC", "SELECT * FROM test WHERE age = 10 ORDER BY a.b.c DESC",
			planner.NewTree(
				planner.NewSortNode(
					planner.NewProjectionNode(
						planner.NewSelectionNode(
							planner.NewTableInputNode("test"),
							expr.Eq(expr.FieldSelector(parsePath(t, "age")), expr.IntegerValue(10)),
						),
						[]planner.ProjectedField{planner.Wildcard{}},
						"test",
					),
					expr.FieldSelector(parsePath(t, "a.b.c")),
					scanner.DESC,
				)),
			false},
		{"WithLimit", "SELECT * FROM test WHERE age = 10 LIMIT 20",
			planner.NewTree(
				planner.NewLimitNode(
					planner.NewProjectionNode(
						planner.NewSelectionNode(
							planner.NewTableInputNode("test"),
							expr.Eq(expr.FieldSelector(parsePath(t, "age")), expr.IntegerValue(10)),
						),
						[]planner.ProjectedField{planner.Wildcard{}},
						"test",
					),
					20,
				)),
			false},
		{"WithOffset", "SELECT * FROM test WHERE age = 10 OFFSET 20",
			planner.NewTree(
				planner.NewOffsetNode(
					planner.NewProjectionNode(
						planner.NewSelectionNode(
							planner.NewTableInputNode("test"),
							expr.Eq(expr.FieldSelector(parsePath(t, "age")), expr.IntegerValue(10)),
						),
						[]planner.ProjectedField{planner.Wildcard{}},
						"test",
					),
					20,
				)),
			false},
		{"WithLimitThenOffset", "SELECT * FROM test WHERE age = 10 LIMIT 10 OFFSET 20",
			planner.NewTree(
				planner.NewLimitNode(
					planner.NewOffsetNode(
						planner.NewProjectionNode(
							planner.NewSelectionNode(
								planner.NewTableInputNode("test"),
								expr.Eq(expr.FieldSelector(parsePath(t, "age")), expr.IntegerValue(10)),
							),
							[]planner.ProjectedField{planner.Wildcard{}},
							"test",
						),
						20,
					),
					10,
				)),
			false},
		{"WithOffsetThenLimit", "SELECT * FROM test WHERE age = 10 OFFSET 20 LIMIT 10", nil, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q, err := ParseQuery(context.Background(), test.s)
			if !test.mustFail {
				require.NoError(t, err)
				require.Len(t, q.Statements, 1)
				require.EqualValues(t, test.expected, q.Statements[0])
			} else {
				require.Error(t, err)
			}
		})
	}
}
