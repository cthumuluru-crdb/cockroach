// Copyright 2017 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package sql

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/cockroachdb/cockroach/pkg/sql/catalog/colinfo"
	"github.com/cockroachdb/cockroach/pkg/sql/clusterunique"
	"github.com/cockroachdb/cockroach/pkg/sql/parser/statements"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
)

// Statement contains a statement with optional expected result columns and metadata.
type Statement struct {
	statements.Statement[tree.Statement]

	StmtNoConstants string
	StmtSummary     string
	QueryID         clusterunique.ID

	ExpectedTypes colinfo.ResultColumns

	// Prepared is non-nil during the PREPARE phase, as well as during EXECUTE of
	// a previously prepared statement. The Prepared statement can be modified
	// during either phase; the PREPARE phase sets its initial state, and the
	// EXECUTE phase can re-prepare it. This happens when the original plan has
	// been invalidated by schema changes, session data changes, permission
	// changes, or other changes to the context in which the original plan was
	// prepared.
	//
	// Given that the PreparedStatement can be modified during planning, it is
	// not safe for use on multiple threads.
	Prepared *PreparedStatement

	// Application specific tags added by SQL commenter
	SQLCommenterTags map[string]string
}

// Parsing log to parse the comments added by
// SQL commenter: https://google.github.io/sqlcommenter/spec/#parsing
func extractSQLCommenterTags(comment string) (map[string]string, error) {
	comment = strings.TrimSpace(comment)
	comment = strings.Replace(comment, "/*", "", 1)
	comment = strings.Replace(comment, "*/", "", 1)

	pairs := strings.Split(comment, ",")
	if len(pairs) == 0 {
		return nil, fmt.Errorf("error parsing SQL comment")
	}

	normalize := func(s string) string {
		if us, err := strconv.Unquote(s); err == nil {
			s = us
		}
		s, _ = url.QueryUnescape(s)
		s, _ = url.PathUnescape(s)
		return s
	}

	sqlCommenterTags := make(map[string]string)
	for _, pair := range pairs {
		elems := strings.Split(pair, "=")
		if len(elems) != 2 {
			return nil, fmt.Errorf("error parsing SQL comment")
		}
		key := normalize(strings.TrimSpace(elems[0]))
		value := normalize(strings.Trim(strings.TrimSpace(elems[1]), "'"))
		sqlCommenterTags[key] = value
	}

	return sqlCommenterTags, nil
}

func makeStatement(
	parserStmt statements.Statement[tree.Statement], queryID clusterunique.ID, fmtFlags tree.FmtFlags,
) Statement {
	sqlCommenterTags := make(map[string]string)
	cl := len(parserStmt.Comments)
	if cl > 0 {
		tags, err := extractSQLCommenterTags(parserStmt.Comments[cl-1])
		if err == nil {
			sqlCommenterTags = tags
		}
	}

	return Statement{
		Statement:        parserStmt,
		StmtNoConstants:  formatStatementHideConstants(parserStmt.AST, fmtFlags),
		StmtSummary:      formatStatementSummary(parserStmt.AST, fmtFlags),
		QueryID:          queryID,
		SQLCommenterTags: sqlCommenterTags,
	}
}

// TODO(chandrat) add SQL commenter tags to prepared statement too.
func makeStatementFromPrepared(prepared *PreparedStatement, queryID clusterunique.ID) Statement {
	return Statement{
		Statement:       prepared.Statement,
		Prepared:        prepared,
		ExpectedTypes:   prepared.Columns,
		StmtNoConstants: prepared.StatementNoConstants,
		StmtSummary:     prepared.StatementSummary,
		QueryID:         queryID,
	}
}

func (s Statement) String() string {
	// We have the original SQL, but we still use String() because it obfuscates
	// passwords.
	return s.AST.String()
}
