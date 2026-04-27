package extraction_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kristofer/codegraph/internal/config"
	"github.com/kristofer/codegraph/internal/extraction"
	"github.com/kristofer/codegraph/internal/types"
)

// =============================================================================
// Mock DB
// =============================================================================

type mockDB struct {
	mu       sync.Mutex
	files    map[string]*types.FileRecord
	nodes    map[string]*types.Node
	edges    []*types.Edge
	unresolved []*types.UnresolvedReference
}

func newMockDB() *mockDB {
	return &mockDB{
		files: make(map[string]*types.FileRecord),
		nodes: make(map[string]*types.Node),
	}
}

func (m *mockDB) UpsertFile(_ context.Context, rec *types.FileRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[rec.Path] = rec
	return nil
}

func (m *mockDB) UpsertNode(_ context.Context, n *types.Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[n.ID] = n
	return nil
}

func (m *mockDB) UpsertEdge(_ context.Context, e *types.Edge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.edges = append(m.edges, e)
	return nil
}

func (m *mockDB) UpsertUnresolvedRef(_ context.Context, ref *types.UnresolvedReference) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unresolved = append(m.unresolved, ref)
	return nil
}

func (m *mockDB) GetFile(_ context.Context, path string) (*types.FileRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.files[path]; ok {
		return rec, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockDB) DeleteNodesForFile(_ context.Context, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, n := range m.nodes {
		if n.FilePath == filePath {
			delete(m.nodes, id)
		}
	}
	return nil
}

// =============================================================================
// Language detection tests
// =============================================================================

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		path string
		want types.Language
	}{
		{"foo.ts", types.TypeScript},
		{"foo.tsx", types.TSX},
		{"foo.js", types.JavaScript},
		{"foo.mjs", types.JavaScript},
		{"foo.jsx", types.JSX},
		{"foo.py", types.Python},
		{"foo.go", types.Go},
		{"foo.rs", types.Rust},
		{"foo.java", types.Java},
		{"foo.c", types.C},
		{"foo.cpp", types.CPP},
		{"foo.cs", types.CSharp},
		{"foo.rb", types.Ruby},
		{"foo.swift", types.Swift},
		{"foo.kt", types.Kotlin},
		{"foo.svelte", types.Svelte},
		{"foo.txt", types.Unknown},
	}
	for _, tc := range cases {
		got := extraction.DetectLanguage(tc.path, nil)
		assert.Equal(t, tc.want, got, "DetectLanguage(%q)", tc.path)
	}
}

func TestDetectLanguageCppHeuristic(t *testing.T) {
	// .h with C++ keywords should detect as CPP
	source := []byte("#pragma once\nclass Foo {};")
	assert.Equal(t, types.CPP, extraction.DetectLanguage("foo.h", source))

	// .h without C++ keywords should remain C
	source2 := []byte("void foo(void);")
	assert.Equal(t, types.C, extraction.DetectLanguage("foo.h", source2))
}

func TestIsLanguageSupported(t *testing.T) {
	assert.True(t, extraction.IsLanguageSupported(types.TypeScript))
	assert.True(t, extraction.IsLanguageSupported(types.Python))
	assert.True(t, extraction.IsLanguageSupported(types.Go))
	assert.True(t, extraction.IsLanguageSupported(types.Rust))
	assert.True(t, extraction.IsLanguageSupported(types.Java))
	assert.False(t, extraction.IsLanguageSupported(types.Dart))
	assert.False(t, extraction.IsLanguageSupported(types.Pascal))
	assert.False(t, extraction.IsLanguageSupported(types.Unknown))
}

// =============================================================================
// TypeScript extraction
// =============================================================================

func TestExtractTypeScript(t *testing.T) {
	source := `export function add(a: number, b: number): number {
    return a + b;
}
export class Calculator {
    multiply(x: number, y: number): number {
        return x * y;
    }
}
import { useState } from 'react';
type MyType = string | number;
interface MyInterface {
    foo(): void;
}
`
	result := extraction.ExtractFromSource("test.ts", []byte(source), types.TypeScript)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)

	byNameKind := indexNodes(result.Nodes)

	// File node should exist
	require.Contains(t, byNameKind, "test.ts:file", "file node should exist")

	// Function 'add' should be found and marked exported
	key := "add:function"
	require.Contains(t, byNameKind, key, "function 'add' should be extracted")
	addFunc := byNameKind[key]
	assert.True(t, addFunc.IsExported, "add should be exported")

	// Class 'Calculator' should be found and exported
	key = "Calculator:class"
	require.Contains(t, byNameKind, key)
	calc := byNameKind[key]
	assert.True(t, calc.IsExported)

	// Method 'multiply' should be found
	key = "multiply:method"
	require.Contains(t, byNameKind, key)

	// Import should be found
	key = "react:import"
	require.Contains(t, byNameKind, key)

	// Type alias
	key = "MyType:type_alias"
	require.Contains(t, byNameKind, key)

	// Interface
	key = "MyInterface:interface"
	require.Contains(t, byNameKind, key)
}

func TestExtractTypeScriptAsync(t *testing.T) {
	source := `async function fetchData(url: string): Promise<string> {
    const res = await fetch(url);
    return res.text();
}
`
	result := extraction.ExtractFromSource("test.ts", []byte(source), types.TypeScript)
	require.NotNil(t, result)

	byNameKind := indexNodes(result.Nodes)
	fn, ok := byNameKind["fetchData:function"]
	require.True(t, ok)
	assert.True(t, fn.IsAsync)
}

// =============================================================================
// Python extraction
// =============================================================================

func TestExtractPython(t *testing.T) {
	source := `import os
from sys import argv

def greet(name):
    """Greet someone."""
    return f"Hello, {name}"

class Animal:
    def __init__(self, name):
        self.name = name

    def speak(self):
        pass

class Dog(Animal):
    def speak(self):
        return "Woof!"
`
	result := extraction.ExtractFromSource("test.py", []byte(source), types.Python)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)

	byNameKind := indexNodes(result.Nodes)

	// Top-level function
	require.Contains(t, byNameKind, "greet:function")

	// Classes
	require.Contains(t, byNameKind, "Animal:class")
	require.Contains(t, byNameKind, "Dog:class")

	// Methods inside classes
	require.Contains(t, byNameKind, "__init__:method")
	require.Contains(t, byNameKind, "speak:method")
}

// =============================================================================
// Go extraction
// =============================================================================

func TestExtractGo(t *testing.T) {
	source := `package main

import "fmt"

type Point struct {
    X, Y int
}

type Stringer interface {
    String() string
}

func (p Point) String() string {
    return fmt.Sprintf("(%d, %d)", p.X, p.Y)
}

func add(a, b int) int {
    return a + b
}
`
	result := extraction.ExtractFromSource("test.go", []byte(source), types.Go)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)

	byNameKind := indexNodes(result.Nodes)

	// Struct
	require.Contains(t, byNameKind, "Point:struct")

	// Interface
	require.Contains(t, byNameKind, "Stringer:interface")

	// Method
	require.Contains(t, byNameKind, "String:method")

	// Function
	require.Contains(t, byNameKind, "add:function")

	// Point is exported (uppercase)
	pointNode := byNameKind["Point:struct"]
	assert.True(t, pointNode.IsExported)

	// add is not exported (lowercase)
	addNode := byNameKind["add:function"]
	assert.False(t, addNode.IsExported)
}

// =============================================================================
// Rust extraction
// =============================================================================

func TestExtractRust(t *testing.T) {
	source := `use std::fmt;

struct Point {
    x: f64,
    y: f64,
}

pub struct Rectangle {
    width: f64,
    height: f64,
}

impl Rectangle {
    pub fn new(w: f64, h: f64) -> Self {
        Rectangle { width: w, height: h }
    }

    pub fn area(&self) -> f64 {
        self.width * self.height
    }
}

fn main() {}
`
	result := extraction.ExtractFromSource("test.rs", []byte(source), types.Rust)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)

	byNameKind := indexNodes(result.Nodes)

	// Structs
	require.Contains(t, byNameKind, "Point:struct")
	require.Contains(t, byNameKind, "Rectangle:struct")

	// Methods from impl block
	require.Contains(t, byNameKind, "new:method")
	require.Contains(t, byNameKind, "area:method")

	// Function
	require.Contains(t, byNameKind, "main:function")

	// Rectangle is pub
	rect := byNameKind["Rectangle:struct"]
	assert.True(t, rect.IsExported)
}

// =============================================================================
// Java extraction
// =============================================================================

func TestExtractJava(t *testing.T) {
	source := `package com.example;

import java.util.List;
import java.util.ArrayList;

public class Calculator {
    private int value;

    public Calculator(int initial) {
        this.value = initial;
    }

    public int add(int a, int b) {
        return a + b;
    }

    private void reset() {
        this.value = 0;
    }
}
`
	result := extraction.ExtractFromSource("Calculator.java", []byte(source), types.Java)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)

	byNameKind := indexNodes(result.Nodes)

	// Class
	require.Contains(t, byNameKind, "Calculator:class")

	// Methods
	require.Contains(t, byNameKind, "add:method")
	require.Contains(t, byNameKind, "reset:method")
}

// =============================================================================
// Edge extraction
// =============================================================================

func TestExtractEdges(t *testing.T) {
	source := `export function outer() {
    function inner() {}
}

export class MyClass {
    myMethod() {}
}
`
	result := extraction.ExtractFromSource("test.ts", []byte(source), types.TypeScript)
	require.NotNil(t, result)

	// Check contains edges exist
	hasContainsEdge := false
	for _, e := range result.Edges {
		if e.Kind == types.EdgeKindContains {
			hasContainsEdge = true
			break
		}
	}
	assert.True(t, hasContainsEdge, "should have contains edges")

	// File node should have contains edges to top-level symbols
	var fileNode *types.Node
	for _, n := range result.Nodes {
		if n.Kind == types.NodeKindFile {
			fileNode = n
			break
		}
	}
	require.NotNil(t, fileNode)

	fileContains := 0
	for _, e := range result.Edges {
		if e.Source == fileNode.ID && e.Kind == types.EdgeKindContains {
			fileContains++
		}
	}
	assert.Greater(t, fileContains, 0, "file node should contain top-level nodes")
}

// =============================================================================
// Orchestrator tests
// =============================================================================

func TestHashContent(t *testing.T) {
	data := []byte("hello world")
	h1 := extraction.HashContent(data)
	h2 := extraction.HashContent(data)
	assert.Equal(t, h1, h2, "hash should be deterministic")
	assert.Len(t, h1, 64, "SHA-256 hex should be 64 chars")

	// Different content → different hash
	h3 := extraction.HashContent([]byte("hello world!"))
	assert.NotEqual(t, h1, h3)
}

func TestScanFiles(t *testing.T) {
	dir := t.TempDir()

	// Create some files
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "utils.ts", "export function foo() {}")
	writeFile(t, dir, "README.md", "# readme")
	writeFile(t, dir, "sub/helper.go", "package sub")
	writeFile(t, dir, "node_modules/pkg/index.js", "module.exports = {}")

	cfg := config.DefaultConfig()
	cfg.RootDir = dir

	db := newMockDB()
	orch := extraction.NewOrchestrator(cfg, db)

	files, err := orch.ScanFiles(context.Background(), dir)
	require.NoError(t, err)

	relFiles := make(map[string]bool, len(files))
	for _, f := range files {
		relFiles[f] = true
	}

	assert.True(t, relFiles["main.go"], "main.go should be scanned")
	assert.True(t, relFiles["utils.ts"], "utils.ts should be scanned")
	assert.True(t, relFiles["sub/helper.go"], "sub/helper.go should be scanned")
	assert.False(t, relFiles["README.md"], "README.md should NOT be scanned (not in include patterns)")
	assert.False(t, relFiles["node_modules/pkg/index.js"], "node_modules should be excluded")
}

func TestParallelIndexing(t *testing.T) {
	dir := t.TempDir()

	// Create multiple Go files
	for i := 0; i < 10; i++ {
		content := fmt.Sprintf(`package pkg

func Func%d() int { return %d }
`, i, i)
		writeFile(t, dir, fmt.Sprintf("file%d.go", i), content)
	}

	cfg := config.DefaultConfig()
	cfg.RootDir = dir
	// Only include Go files for speed
	cfg.Include = []string{"**/*.go"}
	cfg.Exclude = []string{}

	db := newMockDB()
	orch := extraction.NewOrchestrator(cfg, db)

	result, err := orch.IndexAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 10, result.FilesProcessed)
	assert.Empty(t, result.Errors)
	assert.Greater(t, result.NodesExtracted, 0)
}

func TestHashChangeDetection(t *testing.T) {
	dir := t.TempDir()
	filePath := "test.go"
	absPath := filepath.Join(dir, filePath)

	content := `package main

func Hello() string { return "hello" }
`
	require.NoError(t, os.WriteFile(absPath, []byte(content), 0o644))

	cfg := config.DefaultConfig()
	cfg.RootDir = dir
	cfg.Include = []string{"**/*.go"}
	cfg.Exclude = []string{}

	db := newMockDB()
	orch := extraction.NewOrchestrator(cfg, db)

	// First run - file should be processed
	result1, err := orch.IndexAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result1.FilesProcessed)
	assert.Equal(t, 0, result1.FilesSkipped)

	// Second run without changes - file should be skipped
	result2, err := orch.IndexAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, result2.FilesProcessed)
	assert.Equal(t, 1, result2.FilesSkipped)

	// Modify file content
	newContent := content + "\nfunc World() string { return \"world\" }\n"
	require.NoError(t, os.WriteFile(absPath, []byte(newContent), 0o644))

	// Third run - modified file should be processed again
	result3, err := orch.IndexAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result3.FilesProcessed)
	assert.Equal(t, 0, result3.FilesSkipped)
}

func TestUnresolvedRefs(t *testing.T) {
	source := `import { useState, useEffect } from 'react';
import type { FC } from 'react';
`
	result := extraction.ExtractFromSource("component.ts", []byte(source), types.TypeScript)
	require.NotNil(t, result)

	// Import statements should generate unresolved references
	importRefs := 0
	for _, ref := range result.UnresolvedReferences {
		if ref.ReferenceKind == types.EdgeKindImports {
			importRefs++
		}
	}
	assert.Greater(t, importRefs, 0, "import statements should create unresolved references")
}

// =============================================================================
// Helpers
// =============================================================================

func indexNodes(nodes []*types.Node) map[string]*types.Node {
	m := make(map[string]*types.Node, len(nodes))
	for _, n := range nodes {
		key := fmt.Sprintf("%s:%s", n.Name, n.Kind)
		m[key] = n
	}
	return m
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
