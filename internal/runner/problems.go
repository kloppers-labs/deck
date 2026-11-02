package runner

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/charmbracelet/x/ansi"
)

// Problem is a compiler diagnostic recognised in task output.
type Problem struct {
	Path      string
	Line, Col int
	Severity  string
	Code      string
	Msg       string
}

func (p Problem) key() string {
	return fmt.Sprintf("%s:%d:%d:%s:%s", p.Path, p.Line, p.Col, p.Code, p.Msg)
}

type matcher struct {
	re                                   *regexp.Regexp
	path, line, col, severity, code, msg int
}

var matchers = []matcher{
	{re: regexp.MustCompile(`^\s*(?:\d+>)?\s*(\S.*?)\((\d+),(\d+)(?:,\d+,\d+)?\)\s*:\s*(error|warning)\s+([A-Za-z]+\d+)\s*:\s*(.*?)(?:\s+\[[^\]]*\])?\s*$`),
		path: 1, line: 2, col: 3, severity: 4, code: 5, msg: 6},
	{re: regexp.MustCompile(`^\s*(\S.*?\.[cm]?[jt]sx?):(\d+):(\d+)\s+-\s+(error|warning)\s+(TS\d+):\s*(.*?)\s*$`),
		path: 1, line: 2, col: 3, severity: 4, code: 5, msg: 6},
	{re: regexp.MustCompile(`^\s*(\S+\.go):(\d+)(?::(\d+))?:\s+(.*?)\s*$`),
		path: 1, line: 2, col: 3, msg: 4},
}

// matchProblem recognises msbuild/dotnet, tsc and Go diagnostics, ignoring
// ANSI colour.
func matchProblem(line string) (Problem, bool) {
	line = ansi.Strip(line)
	for _, m := range matchers {
		g := m.re.FindStringSubmatch(line)
		if g == nil {
			continue
		}
		p := Problem{Path: g[m.path], Msg: g[m.msg], Severity: "error"}
		p.Line, _ = strconv.Atoi(g[m.line])
		if m.col > 0 && g[m.col] != "" {
			p.Col, _ = strconv.Atoi(g[m.col])
		}
		if m.severity > 0 {
			p.Severity = g[m.severity]
		}
		if m.code > 0 {
			p.Code = g[m.code]
		}
		return p, true
	}
	return Problem{}, false
}
