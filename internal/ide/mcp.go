package ide

import (
	"encoding/json"
)

const protocolVersion = "2025-03-26"

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (b *Bridge) handle(c *client, data []byte) {
	var msg rpcMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	if len(msg.ID) == 0 {
		return
	}
	result, rerr := b.dispatch(msg.Method, msg.Params)
	resp := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
	if rerr != nil {
		resp["error"] = rerr
	} else {
		resp["result"] = result
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return
	}
	c.send(out)
}

func (b *Bridge) dispatch(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": ideName, "version": b.opts.Version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolList()}, nil
	case "tools/call":
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &call); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		return b.callTool(call.Name, call.Arguments)
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
}

func textResult(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func jsonResult(v any) map[string]any {
	data, _ := json.Marshal(v)
	return textResult(string(data))
}

func (b *Bridge) notify(method string, params any) int {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return 0
	}
	return b.broadcast(data)
}

// Mention points connected Claude sessions at lines of path (0-based, inclusive);
// it reports false when no session is connected.
func (b *Bridge) Mention(path string, lineStart, lineEnd int) bool {
	if b == nil || b.Clients() == 0 {
		return false
	}
	return b.notify("at_mentioned", map[string]any{"filePath": path, "lineStart": lineStart, "lineEnd": lineEnd}) > 0
}
