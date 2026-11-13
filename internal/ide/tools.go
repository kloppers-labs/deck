package ide

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kloppers-labs/deck/internal/control"
	"github.com/kloppers-labs/deck/internal/presence"
)

const requestTimeout = 2 * time.Second

// openDiffMode is how deck answers Claude's openDiff: "unlisted" leaves it out
// of tools/list; "pending" holds the call until close_tab and then rejects it.
var openDiffMode = "unlisted"

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var str = map[string]any{"type": "string"}

func toolList() []map[string]any {
	tools := []map[string]any{
		{"name": "openFile", "description": "Open a file in deck, optionally selecting from startText to endText",
			"inputSchema": schema(map[string]any{"filePath": str, "preview": map[string]any{"type": "boolean"},
				"startText": str, "endText": str, "selectToEndOfLine": map[string]any{"type": "boolean"},
				"makeFrontmost": map[string]any{"type": "boolean"}}, "filePath")},
		{"name": "getCurrentSelection", "description": "The selection in deck's open file", "inputSchema": schema(map[string]any{})},
		{"name": "getLatestSelection", "description": "The most recent selection in deck", "inputSchema": schema(map[string]any{})},
		{"name": "getOpenEditors", "description": "Files recently open in deck", "inputSchema": schema(map[string]any{})},
		{"name": "getWorkspaceFolders", "description": "deck's project folder", "inputSchema": schema(map[string]any{})},
		{"name": "getDiagnostics", "description": "Language-server diagnostics deck has seen",
			"inputSchema": schema(map[string]any{"uri": str})},
		{"name": "checkDocumentDirty", "description": "deck never holds unsaved text",
			"inputSchema": schema(map[string]any{"filePath": str}, "filePath")},
		{"name": "saveDocument", "description": "deck is read-only",
			"inputSchema": schema(map[string]any{"filePath": str}, "filePath")},
		{"name": "close_tab", "description": "Close a diff tab", "inputSchema": schema(map[string]any{"tab_name": str}, "tab_name")},
		{"name": "closeAllDiffTabs", "description": "Close every diff tab", "inputSchema": schema(map[string]any{})},
	}
	if openDiffMode == "pending" {
		tools = append(tools, map[string]any{"name": "openDiff", "description": "Show a proposed edit",
			"inputSchema": schema(map[string]any{"old_file_path": str, "new_file_path": str,
				"new_file_contents": str, "tab_name": str}, "old_file_path", "new_file_path", "new_file_contents", "tab_name")})
	}
	return tools
}

func (b *Bridge) callTool(name string, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		FilePath          string `json:"filePath"`
		StartText         string `json:"startText"`
		EndText           string `json:"endText"`
		SelectToEndOfLine bool   `json:"selectToEndOfLine"`
		MakeFrontmost     *bool  `json:"makeFrontmost"`
		URI               string `json:"uri"`
		TabName           string `json:"tab_name"`
	}
	if len(raw) > 0 {
		json.Unmarshal(raw, &args)
	}
	switch name {
	case "openFile":
		return b.openFile(args.FilePath, args.StartText, args.EndText, args.SelectToEndOfLine, args.MakeFrontmost == nil || *args.MakeFrontmost), nil
	case "getCurrentSelection":
		return b.currentSelection("No active editor found"), nil
	case "getLatestSelection":
		return b.currentSelection("No selection available"), nil
	case "getOpenEditors":
		return jsonResult(map[string]any{"tabs": b.openEditors()}), nil
	case "getWorkspaceFolders":
		root := b.opts.Root
		return jsonResult(map[string]any{"success": true, "rootPath": root,
			"folders": []map[string]any{{"name": filepath.Base(root), "uri": fileURL(root), "path": root}}}), nil
	case "getDiagnostics":
		return jsonResult(b.diagnostics(strings.TrimPrefix(args.URI, "file://"))), nil
	case "checkDocumentDirty":
		return jsonResult(map[string]any{"success": true, "filePath": args.FilePath, "isDirty": false, "isUntitled": false}), nil
	case "saveDocument":
		return jsonResult(map[string]any{"success": true, "filePath": args.FilePath, "saved": false, "message": "deck is read-only"}), nil
	case "close_tab":
		b.closeDiff(args.TabName)
		return textResult("TAB_CLOSED"), nil
	case "closeAllDiffTabs":
		return textResult("CLOSED_0_DIFF_TABS"), nil
	case "openDiff":
		if openDiffMode == "pending" {
			return textResult(b.awaitDiff(raw)), nil
		}
	}
	return nil, &rpcError{Code: -32602, Message: "unknown tool: " + name}
}

func fileURL(path string) string { return "file://" + path }

func (b *Bridge) request(req control.Request) control.Reply {
	reply := make(chan control.Reply, 1)
	go b.opts.Send(control.RequestMsg{Req: req, Reply: reply, Deadline: time.Now().Add(requestTimeout)})
	select {
	case r := <-reply:
		return r
	case <-time.After(requestTimeout):
		return control.Reply{Error: "deck is busy: editor open"}
	case <-b.ctx.Done():
		return control.Reply{Error: errClosed.Error()}
	}
}

func (b *Bridge) openFile(path, startText, endText string, toEOL, frontmost bool) any {
	req := control.Request{V: control.Version, Cmd: "open", Path: path, From: "Claude"}
	data, _ := os.ReadFile(path)
	if startText != "" {
		if i := bytes.Index(data, []byte(startText)); i >= 0 {
			req.Line, req.Col = position(data, i)
			if endText != "" {
				from := i + len(startText)
				if j := bytes.Index(data[from:], []byte(endText)); j >= 0 {
					req.EndLine, _ = position(data, from+j+len(endText)-1)
				}
			} else if toEOL {
				req.EndLine = req.Line
			}
		}
	}
	reply := b.request(req)
	if !reply.OK {
		return textResult("Failed to open file: " + reply.Error)
	}
	if frontmost {
		return textResult("Opened file: " + path)
	}
	return jsonResult(map[string]any{"success": true, "filePath": path,
		"languageId": languageID(path), "lineCount": bytes.Count(data, []byte{'\n'}) + 1})
}

// position is the 1-based line and rune column of byte offset i in data.
func position(data []byte, i int) (int, int) {
	before := data[:i]
	line := bytes.Count(before, []byte{'\n'}) + 1
	start := bytes.LastIndexByte(before, '\n') + 1
	return line, utf8.RuneCount(before[start:]) + 1
}

func (b *Bridge) currentSelection(missing string) any {
	payload, _, ok := b.selectionPayload(b.opts.Store.Get())
	if !ok {
		return jsonResult(map[string]any{"success": false, "message": missing})
	}
	payload["success"] = true
	return jsonResult(payload)
}

func (b *Bridge) openEditors() []map[string]any {
	s := b.opts.Store.Get()
	var tabs []map[string]any
	seen := map[string]bool{}
	add := func(path string, active bool) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		tabs = append(tabs, map[string]any{"uri": fileURL(path), "isActive": active,
			"label": filepath.Base(path), "languageId": languageID(path), "isDirty": false})
	}
	add(s.File, true)
	for _, p := range s.Recent {
		add(p, false)
	}
	return tabs
}

var severityLabels = map[string]string{"error": "Error", "warning": "Warning", "information": "Information", "hint": "Hint"}

func (b *Bridge) diagnostics(path string) []map[string]any {
	out := []map[string]any{}
	for p, ds := range b.opts.Store.Diagnostics(path) {
		items := make([]map[string]any, 0, len(ds))
		for _, d := range ds {
			items = append(items, map[string]any{
				"message": d.Message, "severity": severityLabel(d), "source": d.Source,
				"range": map[string]any{
					"start": map[string]any{"line": d.Line - 1, "character": d.Col - 1},
					"end":   map[string]any{"line": d.EndLine - 1, "character": d.EndCol - 1},
				},
			})
		}
		out = append(out, map[string]any{"uri": fileURL(p), "diagnostics": items})
	}
	return out
}

func severityLabel(d presence.Diagnostic) string {
	if l, ok := severityLabels[d.Severity]; ok {
		return l
	}
	return "Error"
}

var languageIDs = map[string]string{
	".go": "go", ".ts": "typescript", ".tsx": "typescriptreact", ".js": "javascript", ".jsx": "javascriptreact",
	".py": "python", ".cs": "csharp", ".rs": "rust", ".md": "markdown", ".json": "json", ".yaml": "yaml",
	".yml": "yaml", ".sh": "shellscript", ".lua": "lua", ".html": "html", ".css": "css", ".toml": "toml",
}

func languageID(path string) string {
	if id, ok := languageIDs[strings.ToLower(filepath.Ext(path))]; ok {
		return id
	}
	return "plaintext"
}
