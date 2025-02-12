// Copyright 2015 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

//go:build bazel

package bazel

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	inner "github.com/bazelbuild/rules_go/go/tools/bazel"
)

// bazelTestEnvVar can be used to determine when running in the `bazel test`
// environment.
const bazelTestEnvVar = "BAZEL_TEST"

// BuiltWithBazel returns true iff this library was built with Bazel.
func BuiltWithBazel() bool {
	return true
}

// InBazelTest returns true iff called from a test run by Bazel.
func InBazelTest() bool {
	if bazelTestEnv, ok := os.LookupEnv(bazelTestEnvVar); ok {
		if bazelTest, err := strconv.ParseBool(bazelTestEnv); err == nil {
			return bazelTest
		}
	}

	return false
}

// InTestWrapper returns true iff called from Bazel's generated test wrapper.
// When enabled and running under `bazel test`, the entire test runs using a
// Bazel-generated wrapper. This wrapper imports the test module, so any
// import-time code will be run twice: once under the wrapper, and once by the
// test process itself. Hence, checking can be helpful for any module
// import-time code, such as init() or any global variable initialization.
// For more info, see:
// https://github.com/bazelbuild/rules_go/blob/master/docs/go/core/rules.md#go_test
//
// Duplicates logic from rules_go's bzltestutil.ShouldWrap(), but does not
// import in order to avoid a dependency (and its global initialization code).
func InTestWrapper() bool {
	if !InBazelTest() {
		return false
	}

	if wrapEnv, ok := os.LookupEnv("GO_TEST_WRAP"); ok {
		wrap, err := strconv.ParseBool(wrapEnv)
		if err != nil {
			return false
		}
		return wrap
	}
	_, ok := os.LookupEnv("XML_OUTPUT_FILE")
	return ok
}

// FindBinary is a convenience wrapper around the rules_go variant.
func FindBinary(pkg, name string) (string, bool) {
	return inner.FindBinary(pkg, name)
}

// Runfile is a convenience wrapper around the rules_go variant.
func Runfile(path string) (string, error) {
	return inner.Runfile(path)
}

// RunfilesPath is a convenience wrapper around the rules_go variant.
func RunfilesPath() (string, error) {
	return inner.RunfilesPath()
}

// TestTmpDir is a convenience wrapper around the rules_go variant.
func TestTmpDir() string {
	return inner.TestTmpDir()
}

// NewTmpDir is a convenience wrapper around the rules_go variant.
// The caller is responsible for cleaning the directory up after use.
func NewTmpDir(prefix string) (string, error) {
	return inner.NewTmpDir(prefix)
}

// Updates the current environment to use the Go toolchain that Bazel built this
// binary/test with (updates the `PATH`/`GOROOT`/`GOCACHE` environment
// variables).
// If you want to use this function, your binary/test target MUST have
// `@go_sdk//:files` in its `data` -- this will make sure the whole toolchain
// gets pulled into the sandbox as well. Generally, this function should only
// be called in init().
func SetGoEnv() {
	gobin, err := Runfile("bin/go")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("PATH", fmt.Sprintf("%s%c%s", filepath.Dir(gobin), os.PathListSeparator, os.Getenv("PATH"))); err != nil {
		panic(err)
	}
	// GOPATH has to be set to some value (not equal to GOROOT) in order for `go env` to work.
	// See https://github.com/golang/go/issues/43938 for the details.
	// Specify a name under the system TEMP/TMP directory in order to be platform agnostic.
	if err := os.Setenv("GOPATH", filepath.Join(os.TempDir(), "nonexist-gopath")); err != nil {
		panic(err)
	}
	if err := os.Setenv("GOROOT", filepath.Dir(filepath.Dir(gobin))); err != nil {
		panic(err)
	}
	if err := os.Setenv("GOCACHE", path.Join(inner.TestTmpDir(), ".gocache")); err != nil {
		panic(err)
	}
}

// Name of the environment variable containing the bazel target path
// (//pkg/cmd/foo:bar).
const testTargetEnv = "TEST_TARGET"

// RelativeTestTargetPath returns relative path to the package
// of the current test.
func RelativeTestTargetPath() string {
	target := os.Getenv(testTargetEnv)
	if target == "" {
		return ""
	}

	// Drop target name.
	if last := strings.LastIndex(target, ":"); last > 0 {
		target = target[:last]
	}
	return strings.TrimPrefix(target, "//")
}
