package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

type testResult struct {
	Package string
	License string
	Score   int
	Extra   int
	Missing int
	Err     string
}

func listTestLicenses(pkgs []string) ([]testResult, error) {
	gopath, err := filepath.Abs("testdata")
	if err != nil {
		return nil, err
	}
	licenses, err := listLicenses(gopath, pkgs)
	if err != nil {
		return nil, err
	}
	res := []testResult{}
	for _, l := range licenses {
		r := testResult{
			Package: l.Package,
		}
		if l.Template != nil {
			r.License = l.Template.Title
			r.Score = int(100 * l.Score)
		}
		if l.Err != "" {
			r.Err = "some error"
		}
		r.Extra = len(l.ExtraWords)
		r.Missing = len(l.MissingWords)
		res = append(res, r)
	}
	return res, nil
}

func compareTestLicenses(pkgs []string, wanted []testResult) error {
	stringify := func(res []testResult) string {
		parts := []string{}
		for _, r := range res {
			s := fmt.Sprintf("%s \"%s\" %d%%", r.Package, r.License, r.Score)
			if r.Err != "" {
				s += " " + r.Err
			}
			if r.Extra > 0 {
				s += fmt.Sprintf(" +%d", r.Extra)
			}
			if r.Missing > 0 {
				s += fmt.Sprintf(" -%d", r.Missing)
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, "\n")
	}

	licenses, err := listTestLicenses(pkgs)
	if err != nil {
		return err
	}
	got := stringify(licenses)
	expected := stringify(wanted)
	if got != expected {
		return fmt.Errorf("licenses do not match:\n%s\n!=\n%s", got, expected)
	}
	return nil
}

func TestNoDependencies(t *testing.T) {
	err := compareTestLicenses([]string{"colors/red"}, []testResult{
		{Package: "colors/red", License: "MIT License", Score: 98, Missing: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMultipleLicenses(t *testing.T) {
	err := compareTestLicenses([]string{"colors/blue"}, []testResult{
		{Package: "colors/blue", License: "Apache License 2.0", Score: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoLicense(t *testing.T) {
	err := compareTestLicenses([]string{"colors/green"}, []testResult{
		{Package: "colors/green", License: "", Score: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMainWithDependencies(t *testing.T) {
	// It also tests license retrieval in parent directory.
	err := compareTestLicenses([]string{"colors/cmd/paint"}, []testResult{
		{Package: "colors/cmd/paint", License: "Academic Free License v3.0", Score: 100},
		{Package: "colors/red", License: "MIT License", Score: 98, Missing: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMainWithAliasedDependencies(t *testing.T) {
	err := compareTestLicenses([]string{"colors/cmd/mix"}, []testResult{
		{Package: "colors/cmd/mix", License: "Academic Free License v3.0", Score: 100},
		{Package: "colors/red", License: "MIT License", Score: 98, Missing: 2},
		{Package: "couleurs/red", License: "GNU Lesser General Public License v2.1",
			Score: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMissingPackage(t *testing.T) {
	_, err := listTestLicenses([]string{"colors/missing"})
	if err == nil {
		t.Fatal("no error on missing package")
	}
	if _, ok := err.(*MissingError); !ok {
		t.Fatalf("MissingError expected")
	}
}

func TestMismatch(t *testing.T) {
	err := compareTestLicenses([]string{"colors/yellow"}, []testResult{
		{Package: "colors/yellow", License: "Microsoft Reciprocal License", Score: 25,
			Extra: 106, Missing: 131},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoBuildableGoSourceFiles(t *testing.T) {
	_, err := listTestLicenses([]string{"colors/cmd"})
	if err == nil {
		t.Fatal("no error on missing package")
	}
	if _, ok := err.(*MissingError); !ok {
		t.Fatalf("MissingError expected")
	}
}

func TestBroken(t *testing.T) {
	err := compareTestLicenses([]string{"colors/broken"}, []testResult{
		{Package: "colors/broken", License: "GNU General Public License v3.0", Score: 100},
		{Package: "colors/missing", License: "", Score: 0, Err: "some error"},
		{Package: "colors/red", License: "MIT License", Score: 98, Missing: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBrokenDependency(t *testing.T) {
	err := compareTestLicenses([]string{"colors/purple"}, []testResult{
		{Package: "colors/broken", License: "GNU General Public License v3.0", Score: 100},
		{Package: "colors/missing", License: "", Score: 0, Err: "some error"},
		{Package: "colors/purple", License: "", Score: 0},
		{Package: "colors/red", License: "MIT License", Score: 98, Missing: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPackageExpression(t *testing.T) {
	err := compareTestLicenses([]string{"colors/cmd/..."}, []testResult{
		{Package: "colors/cmd/mix", License: "Academic Free License v3.0", Score: 100},
		{Package: "colors/cmd/paint", License: "Academic Free License v3.0", Score: 100},
		{Package: "colors/red", License: "MIT License", Score: 98, Missing: 2},
		{Package: "couleurs/red", License: "GNU Lesser General Public License v2.1",
			Score: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCleanLicenseData(t *testing.T) {
	data := `The MIT License (MIT)

	Copyright (c) 2013 Ben Johnson
	
	Some other lines.
	And more.
	`
	cleaned := string(cleanLicenseData([]byte(data)))
	wanted := "the mit license (mit)\n\t\n\tsome other lines.\n\tand more.\n\t"
	if wanted != cleaned {
		t.Fatalf("license data mismatch: %q\n!=\n%q", cleaned, wanted)
	}
}

func TestStandardPackages(t *testing.T) {
	err := compareTestLicenses([]string{"encoding/json", "cmd/addr2line"}, []testResult{})
	if err != nil {
		t.Fatal(err)
	}
}
