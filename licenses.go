package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
)

type Template struct {
	Title    string
	Nickname string
	Text     []byte
}

func parseTemplate(path string) (*Template, error) {
	fp, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	t := Template{}
	state := 0
	defer fp.Close()
	scanner := bufio.NewScanner(fp)
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
			t.Text = append(t.Text, scanner.Bytes()...)
			t.Text = append(t.Text, []byte("\n")...)
		}
	}
	return &t, scanner.Err()
}

func loadTemplates(dir string) ([]*Template, error) {
	templates := []*Template{}
	err := filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || !fi.Mode().IsRegular() {
			return err
		}
		templ, err := parseTemplate(path)
		if err != nil {
			return err
		}
		templates = append(templates, templ)
		return nil
	})
	return templates, err
}

var (
	reWords     = regexp.MustCompile(`[\w']+`)
	reCopyright = regexp.MustCompile(
		`\s*Copyright (©|\(c\)|\xC2\xA9)? ?(\d{4}|\[year\])(.*)?\s*`)
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

func matchTemplates(license []byte, templates []*Template) (*Template, float64) {
	bestScore := float64(-1)
	var bestTemplate *Template
	words := makeWordSet(license)
	for _, t := range templates {
		templWords := makeWordSet(t.Text)
		common := 0
		for w := range words {
			if _, ok := templWords[w]; ok {
				common++
			}
		}
		score := 2 * float64(common) / (float64(len(words)) + float64(len(templWords)))
		if score > bestScore {
			bestScore = score
			bestTemplate = t
		}
	}
	return bestTemplate, bestScore
}

func listDependencies(pkg string) ([]string, error) {
	templ := "{{range .Deps}}{{.}}|{{end}}"
	cmd := exec.Command("go", "list", "-f", templ, pkg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("'go list -f %s %s' failed with: %s", templ, pkg, err)
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

func listStandardPackages() ([]string, error) {
	cmd := exec.Command("go", "list", "std")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go list std failed with: %s", err)
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

func getPackagesInfo(pkgs []string) ([]*PkgInfo, error) {
	args := []string{"list", "-json"}
	// TODO: split the list for platforms which do not support massive argument
	// lists.
	args = append(args, pkgs...)
	cmd := exec.Command("go", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go list -json failed with: %s", err)
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
	reLicense = regexp.MustCompile(`(?i)^(` +
		`((?:un)?licen[sc]e)|` +
		`((?:un)?licen[sc]e\.(?:md|markdown|txt))|` +
		`(copy(?:ing|right)(?:\.[^.]+)?)|` +
		`(licen[sc]e\.[^.]+)` +
		`)$`)
)

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

func listLicenses(args []string) error {
	confidence := 0.9
	pkg := args[0]
	templates, err := loadTemplates("templates")
	if err != nil {
		return err
	}
	deps, err := listDependencies(pkg)
	if err != nil {
		return fmt.Errorf("could not list %s dependencies: %s", pkg, err)
	}
	deps = append(deps, pkg)
	std, err := listStandardPackages()
	if err != nil {
		return fmt.Errorf("could not list standard packages: %s", err)
	}
	stdSet := map[string]bool{}
	for _, n := range std {
		stdSet[n] = true
	}
	w := tabwriter.NewWriter(os.Stdout, 1, 4, 2, ' ', 0)
	infos, err := getPackagesInfo(deps)
	if err != nil {
		return err
	}
	for _, info := range infos {
		if stdSet[info.ImportPath] {
			continue
		}
		path, err := findLicense(info)
		if err != nil {
			return err
		}
		license := "?"
		if path != "" {
			data, err := ioutil.ReadFile(filepath.Join(info.Root, "src", path))
			if err != nil {
				return err
			}
			t, score := matchTemplates(data, templates)
			if score >= confidence {
				license = fmt.Sprintf("%s (%2d%%)", t.Title, int(100*score))
			} else {
				license = fmt.Sprintf("? (%s, %2d%%)", t.Title, int(100*score))
			}
		}
		w.Write([]byte(info.ImportPath + "\t" + license + "\n"))
	}
	return w.Flush()
}

func main() {
	err := listLicenses(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
