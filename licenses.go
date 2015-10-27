package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/pmezard/licenses/assets"
)

type Template struct {
	Title    string
	Nickname string
	Words    map[string]bool
}

func parseTemplate(content string) (*Template, error) {
	t := Template{}
	text := []byte{}
	state := 0
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if state == 0 {
			if line == "---" {
				state = 1
			}
		} else if state == 1 {
			if line == "---" {
				state = 2
			} else {
				if strings.HasPrefix(line, "title:") {
					t.Title = strings.TrimSpace(line[len("title:"):])
				} else if strings.HasPrefix(line, "nickname:") {
					t.Nickname = strings.TrimSpace(line[len("nickname:"):])
				}
			}
		} else if state == 2 {
			text = append(text, scanner.Bytes()...)
			text = append(text, []byte("\n")...)
		}
	}
	t.Words = makeWordSet(text)
	return &t, scanner.Err()
}

func loadTemplates() ([]*Template, error) {
	templates := []*Template{}
	for _, a := range assets.Assets {
		templ, err := parseTemplate(a.Content)
		if err != nil {
			return nil, err
		}
		templates = append(templates, templ)
	}
	return templates, nil
}

var (
	reWords     = regexp.MustCompile(`[\w']+`)
	reCopyright = regexp.MustCompile(
		`\s*Copyright (?:©|\(c\)|\xC2\xA9)? ?(?:\d{4}|\[year\]).*?\s*`)
)

func makeWordSet(data []byte) map[string]bool {
	words := map[string]bool{}
	data = bytes.ToLower(data)
	data = reCopyright.ReplaceAll(data, nil)
	matches := reWords.FindAll(data, -1)
	for _, m := range matches {
		words[string(m)] = true
	}
	return words
}

// matchTemplates returns the best license template matching supplied data, and
// its score between 0 and 1.
func matchTemplates(license []byte, templates []*Template) (*Template, float64) {
	bestScore := float64(-1)
	var bestTemplate *Template
	words := makeWordSet(license)
	for _, t := range templates {
		common := 0
		for w := range words {
			if _, ok := t.Words[w]; ok {
				common++
			}
		}
		score := 2 * float64(common) / (float64(len(words)) + float64(len(t.Words)))
		if score > bestScore {
			bestScore = score
			bestTemplate = t
		}
	}
	return bestTemplate, bestScore
}

// fixEnv returns a copy of the process environment where GOPATH is adjusted to
// supplied value. It returns nil if gopath is empty.
func fixEnv(gopath string) []string {
	if gopath == "" {
		return nil
	}
	kept := []string{
		"GOPATH=" + gopath,
	}
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "GOPATH=") {
			kept = append(kept, env)
		}
	}
	return kept
}

type MissingError struct {
	Err string
}

func (err *MissingError) Error() string {
	return err.Err
}

func listDependencies(gopath, pkg string) ([]string, error) {
	templ := "{{range .Deps}}{{.}}|{{end}}"
	cmd := exec.Command("go", "list", "-f", templ, pkg)
	cmd.Env = fixEnv(gopath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "cannot find package") {
			return nil, &MissingError{Err: string(out)}
		}
		return nil, fmt.Errorf("'go list -f %s %s' failed with:\n%s",
			templ, pkg, string(out))
	}
	deps := []string{}
	for _, s := range strings.Split(string(out), "|") {
		s = strings.TrimSpace(s)
		if s != "" {
			deps = append(deps, s)
		}
	}
	sort.Strings(deps)
	return deps, nil
}

func listStandardPackages(gopath string) ([]string, error) {
	cmd := exec.Command("go", "list", "std")
	cmd.Env = fixEnv(gopath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go list std failed with:\n%s", string(out))
	}
	names := []string{}
	for _, s := range strings.Split(string(out), "\n") {
		s = strings.TrimSpace(s)
		if s != "" {
			names = append(names, s)
		}
	}
	return names, nil
}

type PkgInfo struct {
	Name       string
	Dir        string
	Root       string
	ImportPath string
}

func getPackagesInfo(gopath string, pkgs []string) ([]*PkgInfo, error) {
	args := []string{"list", "-json"}
	// TODO: split the list for platforms which do not support massive argument
	// lists.
	args = append(args, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Env = fixEnv(gopath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go %s failed with:\n%s",
			strings.Join(args, " "), string(out))
	}
	infos := make([]*PkgInfo, 0, len(pkgs))
	decoder := json.NewDecoder(bytes.NewBuffer(out))
	for _, pkg := range pkgs {
		info := &PkgInfo{}
		err := decoder.Decode(info)
		if err != nil {
			return nil, fmt.Errorf("could not retrieve package information for %s", pkg)
		}
		if pkg != info.ImportPath {
			return nil, fmt.Errorf("package information mismatch: asked for %s, got %s",
				pkg, info.ImportPath)
		}
		infos = append(infos, info)
	}
	return infos, err
}

var (
	licenseFiles = []string{
		"LICENSE",
		"LICENSE.txt",
		"LICENSE.md",
		"COPYING",
		"COPYRIGHT",
	}
	reLicense = regexp.MustCompile(`(?i)^(?:` +
		`((?:un)?licen[sc]e)|` +
		`((?:un)?licen[sc]e\.(?:md|markdown|txt))|` +
		`(copy(?:ing|right)(?:\.[^.]+)?)|` +
		`(licen[sc]e\.[^.]+)` +
		`)$`)
)

// scoreLicenseName returns a factor between 0 and 1 weighting how likely
// supplied filename is a license file.
func scoreLicenseName(name string) float64 {
	m := reLicense.FindStringSubmatch(name)
	switch {
	case m == nil:
		break
	case m[1] != "":
		return 1.0
	case m[2] != "":
		return 0.9
	case m[3] != "":
		return 0.8
	case m[4] != "":
		return 0.7
	}
	return 0.
}

// findLicense looks for license files in package import path, and down to
// parent directories until a file is found or $GOPATH/src is reached. It
// returns the path and score of the best entry, an empty string if none was
// found.
func findLicense(info *PkgInfo) (string, error) {
	path := info.ImportPath
	for ; path != "."; path = filepath.Dir(path) {
		fis, err := ioutil.ReadDir(filepath.Join(info.Root, "src", path))
		if err != nil {
			return "", err
		}
		bestScore := float64(0)
		bestName := ""
		for _, fi := range fis {
			if !fi.Mode().IsRegular() {
				continue
			}
			score := scoreLicenseName(fi.Name())
			if score > bestScore {
				bestScore = score
				bestName = fi.Name()
			}
		}
		if bestName != "" {
			return filepath.Join(path, bestName), nil
		}
	}
	return "", nil
}

type License struct {
	Package  string
	Score    float64
	Template *Template
	Path     string
}

func listLicenses(gopath, pkg string) ([]License, error) {
	templates, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	deps, err := listDependencies(gopath, pkg)
	if err != nil {
		if _, ok := err.(*MissingError); ok {
			return nil, err
		}
		return nil, fmt.Errorf("could not list %s dependencies: %s", pkg, err)
	}
	deps = append(deps, pkg)
	std, err := listStandardPackages(gopath)
	if err != nil {
		return nil, fmt.Errorf("could not list standard packages: %s", err)
	}
	stdSet := map[string]bool{}
	for _, n := range std {
		stdSet[n] = true
	}
	infos, err := getPackagesInfo(gopath, deps)
	if err != nil {
		return nil, err
	}
	licenses := []License{}
	for _, info := range infos {
		if stdSet[info.ImportPath] {
			continue
		}
		path, err := findLicense(info)
		if err != nil {
			return nil, err
		}
		license := License{
			Package: info.ImportPath,
			Path:    path,
		}
		if path != "" {
			data, err := ioutil.ReadFile(filepath.Join(info.Root, "src", path))
			if err != nil {
				return nil, err
			}
			t, score := matchTemplates(data, templates)
			license.Score = score
			license.Template = t
		}
		licenses = append(licenses, license)
	}
	return licenses, nil
}

// longestCommonPrefix returns the longest common prefix over import path
// components of supplied licenses.
func longestCommonPrefix(licenses []License) string {
	type Node struct {
		Name     string
		Children map[string]*Node
	}
	// Build a prefix tree. Not super efficient, but easy to do.
	root := &Node{
		Children: map[string]*Node{},
	}
	for _, l := range licenses {
		n := root
		for _, part := range strings.Split(l.Package, "/") {
			c := n.Children[part]
			if c == nil {
				c = &Node{
					Name:     part,
					Children: map[string]*Node{},
				}
				n.Children[part] = c
			}
			n = c
		}
	}
	n := root
	prefix := []string{}
	for {
		if len(n.Children) != 1 {
			break
		}
		for _, c := range n.Children {
			prefix = append(prefix, c.Name)
			n = c
			break
		}
	}
	return strings.Join(prefix, "/")
}

// groupLicenses returns the input licenses after grouping them by license path
// and find their longest import path common prefix. Entries with empty paths
// are left unchanged.
func groupLicenses(licenses []License) ([]License, error) {
	paths := map[string][]License{}
	for _, l := range licenses {
		if l.Path == "" {
			continue
		}
		paths[l.Path] = append(paths[l.Path], l)
	}
	for k, v := range paths {
		if len(v) <= 1 {
			continue
		}
		prefix := longestCommonPrefix(v)
		if prefix == "" {
			return nil, fmt.Errorf(
				"packages share the same license but not common prefix: %v", v)
		}
		l := v[0]
		l.Package = prefix
		paths[k] = []License{l}
	}
	kept := []License{}
	for _, l := range licenses {
		if l.Path == "" {
			kept = append(kept, l)
			continue
		}
		if v, ok := paths[l.Path]; ok {
			kept = append(kept, v[0])
			delete(paths, l.Path)
		}
	}
	return kept, nil
}

func printLicenses() error {
	flag.Usage = func() {
		fmt.Println(`Usage: licenses IMPORTPATH

licenses lists all dependencies of specified package or command, excluding
standard library packages, and prints their licenses. Licenses are detected by
looking for files named like LICENSE, COPYING, COPYRIGHT and other variants in
the package directory, and its parent directories until one is found. Files
content is matched against a set of well-known licenses and the best match is
displayed along with its score.

With -a, all individual packages are displayed instead of grouping them by
license files.
`)
		os.Exit(1)
	}
	all := flag.Bool("a", false, "display all individual packages")
	flag.Parse()
	if flag.NArg() != 1 {
		return fmt.Errorf("expect a single package argument, got %d", flag.NArg())
	}
	pkg := flag.Arg(0)

	confidence := 0.9
	licenses, err := listLicenses("", pkg)
	if err != nil {
		return err
	}
	if !*all {
		licenses, err = groupLicenses(licenses)
		if err != nil {
			return err
		}
	}
	w := tabwriter.NewWriter(os.Stdout, 1, 4, 2, ' ', 0)
	for _, l := range licenses {
		license := "?"
		if l.Template != nil {
			if l.Score >= confidence {
				license = fmt.Sprintf("%s (%2d%%)", l.Template.Title, int(100*l.Score))
			} else {
				license = fmt.Sprintf("? (%s, %2d%%)", l.Template.Title, int(100*l.Score))
			}
		}
		_, err = w.Write([]byte(l.Package + "\t" + license + "\n"))
		if err != nil {
			return err
		}
	}
	return w.Flush()
}

func main() {
	err := printLicenses()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
