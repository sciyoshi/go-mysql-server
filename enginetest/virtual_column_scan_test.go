// Copyright 2026 Dolthub, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enginetest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dolthub/go-mysql-server/sql"
)

// TestDropAndShowSkipVirtualColumnScans checks that DROP and SHOW statements don't resolve the generated column
// expressions of the tables they target, so that tables whose stored generated expressions can't be resolved, or
// reference their own table, can still be inspected and repaired. See TestGeneratedColumnRecursion for how these tests
// construct such schemas.
func TestDropAndShowSkipVirtualColumnScans(t *testing.T) {
	const selfReference = "(select 1 from t sq)"
	setup := []string{
		"CREATE TABLE t (pk INT PRIMARY KEY, a INT, b INT AS (a + 1) VIRTUAL);",
		"INSERT INTO t (pk, a) VALUES (1, 1), (2, 2);",
		"CREATE INDEX idx ON t ((a + 10));",
	}

	t.Run("SHOW and information_schema", func(t *testing.T) {
		for _, q := range []string{
			"SHOW CREATE TABLE t;",
			"SHOW COLUMNS FROM t;",
			"SHOW FULL COLUMNS FROM t;",
			"SHOW EXTENDED COLUMNS FROM t;",
			"SHOW COLUMNS FROM t LIKE 'b';",
			"SHOW COLUMNS FROM t WHERE Field = 'b';",
			"DESCRIBE t;",
			"SHOW INDEXES FROM t;",
			"SELECT column_name, generation_expression FROM information_schema.columns WHERE table_name = 't';",
		} {
			t.Run(q, func(t *testing.T) {
				engine, ctx := newGeneratedColumnTestEngine(t, setup...)
				setGeneratedExpression(t, ctx, engine, "t", "b", selfReference)
				setGeneratedExpression(t, ctx, engine, "t", "!hidden!idx!0!0", selfReference)
				_, err := runQuery(ctx, engine, q)
				require.NoError(t, err)
			})
		}

		engine, ctx := newGeneratedColumnTestEngine(t, setup...)
		setGeneratedExpression(t, ctx, engine, "t", "b", selfReference)
		rows, err := runQuery(ctx, engine, "SHOW CREATE TABLE t;")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Contains(t, strings.ToLower(rows[0][1].(string)), "select 1 from t")
	})

	t.Run("DROP INDEX removes a functional index that references its own table", func(t *testing.T) {
		for _, q := range []string{"DROP INDEX idx ON t;", "ALTER TABLE t DROP INDEX idx;"} {
			t.Run(q, func(t *testing.T) {
				engine, ctx := newGeneratedColumnTestEngine(t, setup...)
				setGeneratedExpression(t, ctx, engine, "t", "!hidden!idx!0!0", selfReference)
				_, err := runQuery(ctx, engine, q)
				require.NoError(t, err)

				rows, err := runQuery(ctx, engine, "SELECT * FROM t ORDER BY pk;")
				require.NoError(t, err)
				require.Equal(t, []sql.Row{{int32(1), int32(1), int32(2)}, {int32(2), int32(2), int32(3)}}, rows)
			})
		}
	})

	t.Run("DROP COLUMN removes a generated column that references its own table", func(t *testing.T) {
		engine, ctx := newGeneratedColumnTestEngine(t, setup...)
		setGeneratedExpression(t, ctx, engine, "t", "b", selfReference)
		_, err := runQuery(ctx, engine, "ALTER TABLE t DROP COLUMN b;")
		require.NoError(t, err)

		rows, err := runQuery(ctx, engine, "SELECT * FROM t ORDER BY pk;")
		require.NoError(t, err)
		require.Equal(t, []sql.Row{{int32(1), int32(1)}, {int32(2), int32(2)}}, rows)
	})

	t.Run("DROP TABLE", func(t *testing.T) {
		engine, ctx := newGeneratedColumnTestEngine(t, setup...)
		setGeneratedExpression(t, ctx, engine, "t", "b", selfReference)
		setGeneratedExpression(t, ctx, engine, "t", "!hidden!idx!0!0", selfReference)
		_, err := runQuery(ctx, engine, "DROP TABLE t;")
		require.NoError(t, err)

		rows, err := runQuery(ctx, engine, "SHOW TABLES LIKE 't';")
		require.NoError(t, err)
		require.Empty(t, rows)
	})

	t.Run("SHOW COLUMNS filters still read virtual columns", func(t *testing.T) {
		engine, ctx := newGeneratedColumnTestEngine(t, setup...)
		rows, err := runQuery(ctx, engine, "SHOW COLUMNS FROM t WHERE Field IN (SELECT 'b' FROM t WHERE b = 3);")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "b", rows[0][0])
	})
}
