package resolution

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kristofer/codegraph/internal/db"
	"github.com/kristofer/codegraph/internal/types"
)

// ===========================================================================
// Helpers
// ===========================================================================

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.OpenMemory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func sampleNode(id, name, filePath string, kind types.NodeKind, lang types.Language) *types.Node {
	return &types.Node{
		ID:            id,
		Kind:          kind,
		Name:          name,
		QualifiedName: filePath + "::" + name,
		FilePath:      filePath,
		Language:      lang,
		StartLine:     1,
		EndLine:       10,
		UpdatedAt:     time.Now().UnixMilli(),
	}
}

func insertNode(t *testing.T, q *db.Queries, n *types.Node) {
	t.Helper()
	require.NoError(t, q.UpsertNode(n))
}

func insertEdge(t *testing.T, q *db.Queries, src, tgt string, kind types.EdgeKind) {
	t.Helper()
	require.NoError(t, q.InsertEdge(&types.Edge{Source: src, Target: tgt, Kind: kind}))
}

// ===========================================================================
// TestExtractJSImports
// ===========================================================================

func TestExtractJSImports(t *testing.T) {
	content := `
import React from 'react';
import { useState, useEffect } from 'react';
import * as utils from './utils';
import { foo as bar } from './bar';
const baz = require('./baz');
const { a, b } = require('./ab');
`
	mappings := ExtractImportMappings(content, types.TypeScript)
	require.NotEmpty(t, mappings)

	byLocal := make(map[string]ImportMapping, len(mappings))
	for _, m := range mappings {
		byLocal[m.LocalName] = m
	}

	// Default import
	assert.Equal(t, "default", byLocal["React"].ExportedName)
	assert.True(t, byLocal["React"].IsDefault)

	// Named imports
	assert.Equal(t, "useState", byLocal["useState"].ExportedName)
	assert.Equal(t, "useEffect", byLocal["useEffect"].ExportedName)

	// Namespace import
	assert.True(t, byLocal["utils"].IsNamespace)

	// Aliased import
	assert.Equal(t, "foo", byLocal["bar"].ExportedName)

	// require
	assert.Equal(t, "default", byLocal["baz"].ExportedName)
	assert.Equal(t, "a", byLocal["a"].ExportedName)
	assert.Equal(t, "b", byLocal["b"].ExportedName)
}

func TestExtractPythonImports(t *testing.T) {
	content := `
from os.path import join, exists
from typing import List, Optional as Opt
import json
import sys as system
`
	mappings := ExtractImportMappings(content, types.Python)
	require.NotEmpty(t, mappings)

	byLocal := make(map[string]ImportMapping, len(mappings))
	for _, m := range mappings {
		byLocal[m.LocalName] = m
	}

	assert.Equal(t, "join", byLocal["join"].ExportedName)
	assert.Equal(t, "exists", byLocal["exists"].ExportedName)
	assert.Equal(t, "Optional", byLocal["Opt"].ExportedName)
	assert.Equal(t, "json", byLocal["json"].LocalName)
	assert.Equal(t, "system", byLocal["system"].LocalName)
	assert.Equal(t, "sys", byLocal["system"].Source)
}

func TestExtractGoImports(t *testing.T) {
	content := `
package main

import (
	"fmt"
	myfmt "fmt"
	"github.com/example/pkg"
)
`
	mappings := ExtractImportMappings(content, types.Go)
	require.NotEmpty(t, mappings)

	byLocal := make(map[string]ImportMapping, len(mappings))
	for _, m := range mappings {
		byLocal[m.LocalName] = m
	}

	assert.Equal(t, "fmt", byLocal["fmt"].LocalName)
	assert.Equal(t, "myfmt", byLocal["myfmt"].LocalName)
	assert.Equal(t, "pkg", byLocal["pkg"].LocalName)
}

// ===========================================================================
// TestMatchByExactName
// ===========================================================================

func TestMatchByExactName(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	n := sampleNode("fn1", "doSomething", "src/a.ts", types.NodeKindFunction, types.TypeScript)
	insertNode(t, q, n)

	ctx := NewResolutionContext("/project", q)
	ctx.WarmCaches()

	ref := &types.UnresolvedReference{
		FromNodeID:    "caller1",
		ReferenceName: "doSomething",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/a.ts",
		Language:      types.TypeScript,
	}

	result := matchByExactName(ref, ctx)
	require.NotNil(t, result)
	assert.Equal(t, "fn1", result.TargetNodeID)
	assert.Equal(t, "exact-match", result.ResolvedBy)
	assert.GreaterOrEqual(t, result.Confidence, 0.5)
}

// ===========================================================================
// TestMatchMethodCall
// ===========================================================================

func TestMatchMethodCall(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	classNode := sampleNode("cls1", "UserService", "src/user.ts", types.NodeKindClass, types.TypeScript)
	insertNode(t, q, classNode)

	method := sampleNode("mth1", "getUser", "src/user.ts", types.NodeKindMethod, types.TypeScript)
	method.QualifiedName = "src/user.ts::UserService.getUser"
	insertNode(t, q, method)

	ctx := NewResolutionContext("/project", q)

	ref := &types.UnresolvedReference{
		FromNodeID:    "caller1",
		ReferenceName: "UserService.getUser",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/main.ts",
		Language:      types.TypeScript,
	}

	result := matchMethodCall(ref, ctx)
	require.NotNil(t, result)
	assert.Equal(t, "mth1", result.TargetNodeID)
}

// ===========================================================================
// TestMatchFuzzy
// ===========================================================================

func TestMatchFuzzy(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	n := sampleNode("fn1", "MyFunction", "src/a.ts", types.NodeKindFunction, types.TypeScript)
	insertNode(t, q, n)

	ctx := NewResolutionContext("/project", q)

	ref := &types.UnresolvedReference{
		FromNodeID:    "caller1",
		ReferenceName: "myfunction",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/a.ts",
		Language:      types.TypeScript,
	}

	result := matchFuzzy(ref, ctx)
	require.NotNil(t, result)
	assert.Equal(t, "fn1", result.TargetNodeID)
	assert.Equal(t, "fuzzy", result.ResolvedBy)
}

// ===========================================================================
// TestResolveOne_BuiltIn
// ===========================================================================

func TestResolveOne_BuiltIn(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	resolver := NewReferenceResolver("/project", q)

	// console.log should be skipped as a JS built-in
	ref := &types.UnresolvedReference{
		FromNodeID:    "caller1",
		ReferenceName: "console.log",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/a.ts",
		Language:      types.TypeScript,
	}

	result := resolver.ResolveOne(ref)
	assert.Nil(t, result, "expected nil for built-in console.log")
}

// ===========================================================================
// TestResolveOne_ExactMatch
// ===========================================================================

func TestResolveOne_ExactMatch(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	n := sampleNode("fn1", "helperFunc", "src/helpers.ts", types.NodeKindFunction, types.TypeScript)
	insertNode(t, q, n)

	resolver := NewReferenceResolver("/project", q)

	ref := &types.UnresolvedReference{
		FromNodeID:    "caller1",
		ReferenceName: "helperFunc",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/main.ts",
		Language:      types.TypeScript,
	}

	result := resolver.ResolveOne(ref)
	require.NotNil(t, result)
	assert.Equal(t, "fn1", result.TargetNodeID)
}

// ===========================================================================
// TestResolveAndPersist
// ===========================================================================

func TestResolveAndPersist(t *testing.T) {
	d := openTestDB(t)
	q := db.NewQueries(d)

	// Set up a target function
	target := sampleNode("fn1", "targetFunc", "src/target.ts", types.NodeKindFunction, types.TypeScript)
	insertNode(t, q, target)

	// Set up a caller node
	caller := sampleNode("fn2", "callerFunc", "src/caller.ts", types.NodeKindFunction, types.TypeScript)
	insertNode(t, q, caller)

	// Add an unresolved reference from caller to target
	require.NoError(t, q.InsertUnresolvedRef(&types.UnresolvedReference{
		FromNodeID:    "fn2",
		ReferenceName: "targetFunc",
		ReferenceKind: types.EdgeKindCalls,
		Line:          5,
		Column:        2,
		FilePath:      "src/caller.ts",
		Language:      types.TypeScript,
	}))

	resolver := NewReferenceResolver("/project", q)
	stats, err := resolver.ResolveAndPersist()
	require.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, 1, stats.Resolved)
	assert.Equal(t, 0, stats.Unresolved)

	// Verify edge was created
	edges, err := q.GetEdges("fn2", types.EdgeDirectionOutgoing, types.EdgeKindCalls)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, "fn1", edges[0].Target)

	// Verify unresolved_refs was cleaned up
	count, err := q.GetUnresolvedRefsCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// ===========================================================================
// TestIsBuiltInOrExternal
// ===========================================================================

func TestIsBuiltInOrExternal(t *testing.T) {
	tests := []struct {
		name     string
		ref      *types.UnresolvedReference
		expected bool
	}{
		{"JS console", &types.UnresolvedReference{ReferenceName: "console", Language: types.TypeScript}, true},
		{"JS console.log", &types.UnresolvedReference{ReferenceName: "console.log", Language: types.TypeScript}, true},
		{"React useState", &types.UnresolvedReference{ReferenceName: "useState", Language: types.TypeScript}, true},
		{"Python print", &types.UnresolvedReference{ReferenceName: "print", Language: types.Python}, true},
		{"Go fmt.Println", &types.UnresolvedReference{ReferenceName: "fmt.Println", Language: types.Go}, true},
		{"Go make", &types.UnresolvedReference{ReferenceName: "make", Language: types.Go}, true},
		{"User function", &types.UnresolvedReference{ReferenceName: "myFunc", Language: types.TypeScript}, false},
		{"User class", &types.UnresolvedReference{ReferenceName: "MyClass", Language: types.Go}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBuiltInOrExternal(tt.ref)
			assert.Equal(t, tt.expected, result, "isBuiltInOrExternal(%q, %s)", tt.ref.ReferenceName, tt.ref.Language)
		})
	}
}

// ===========================================================================
// TestSplitCamelCase
// ===========================================================================

func TestSplitCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"userService", []string{"user", "Service"}},
		{"UserService", []string{"User", "Service"}},
		{"getHTTPClient", []string{"get", "HTTPClient"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := splitCamelCase(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ===========================================================================
// TestComputePathProximity
// ===========================================================================

func TestComputePathProximity(t *testing.T) {
	tests := []struct {
		a, b           string
		minExpectedScore int
	}{
		{"src/a/b/c.ts", "src/a/b/d.ts", 30},  // 2 shared dirs × 15 = 30
		{"src/a.ts", "lib/b.ts", 0},             // no shared dirs
		{"src/a/b.ts", "src/a/b.ts", 30},        // same dir
	}

	for _, tt := range tests {
		score := computePathProximity(tt.a, tt.b)
		assert.GreaterOrEqual(t, score, tt.minExpectedScore,
			"computePathProximity(%q, %q)", tt.a, tt.b)
	}
}
