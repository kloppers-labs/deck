package lsp

import "encoding/json"

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

func (p Position) Before(q Position) bool {
	return p.Line < q.Line || (p.Line == q.Line && p.Character < q.Character)
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

func (r Range) Contains(o Range) bool {
	return !o.Start.Before(r.Start) && !r.End.Before(o.End)
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type LocationLink struct {
	OriginSelectionRange *Range `json:"originSelectionRange,omitempty"`
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type SymbolKind int

const (
	SymbolFile SymbolKind = iota + 1
	SymbolModule
	SymbolNamespace
	SymbolPackage
	SymbolClass
	SymbolMethod
	SymbolProperty
	SymbolField
	SymbolConstructor
	SymbolEnum
	SymbolInterface
	SymbolFunction
	SymbolVariable
	SymbolConstant
	SymbolString
	SymbolNumber
	SymbolBoolean
	SymbolArray
	SymbolObject
	SymbolKey
	SymbolNull
	SymbolEnumMember
	SymbolStruct
	SymbolEvent
	SymbolOperator
	SymbolTypeParameter
)

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type SymbolInformation struct {
	Name          string     `json:"name"`
	Kind          SymbolKind `json:"kind"`
	Location      Location   `json:"location"`
	ContainerName string     `json:"containerName,omitempty"`
}

// Symbol is a decoded symbol flattened in DFS order; Parent indexes the same slice, -1 at top level.
type Symbol struct {
	Name           string
	Detail         string
	Kind           SymbolKind
	URI            string
	Range          Range
	SelectionRange Range
	Container      string
	Depth          int
	Parent         int
}

type Hover struct {
	Markdown string
	Range    *Range
}

type DiagnosticSeverity int

const (
	SeverityError DiagnosticSeverity = iota + 1
	SeverityWarning
	SeverityInformation
	SeverityHint
)

type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Code     json.RawMessage    `json:"code,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type ProgressParams struct {
	Token json.RawMessage `json:"token"`
	Value json.RawMessage `json:"value"`
}

type WorkDoneProgress struct {
	Kind        string `json:"kind"`
	Title       string `json:"title,omitempty"`
	Message     string `json:"message,omitempty"`
	Percentage  *int   `json:"percentage,omitempty"`
	Cancellable bool   `json:"cancellable,omitempty"`
}

type WorkDoneProgressCreateParams struct {
	Token json.RawMessage `json:"token"`
}

type MessageType int

const (
	MessageError MessageType = iota + 1
	MessageWarning
	MessageInfo
	MessageLog
)

type ShowMessageParams struct {
	Type    MessageType `json:"type"`
	Message string      `json:"message"`
}

type ConfigurationItem struct {
	ScopeURI string `json:"scopeUri,omitempty"`
	Section  string `json:"section,omitempty"`
}

type ConfigurationParams struct {
	Items []ConfigurationItem `json:"items"`
}

type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type InitializeParams struct {
	ProcessID             int               `json:"processId"`
	ClientInfo            ClientInfo        `json:"clientInfo"`
	RootURI               string            `json:"rootUri"`
	RootPath              string            `json:"rootPath"`
	WorkspaceFolders      []WorkspaceFolder `json:"workspaceFolders"`
	Capabilities          map[string]any    `json:"capabilities"`
	InitializationOptions any               `json:"initializationOptions,omitempty"`
}

type ServerCapabilities struct {
	PositionEncoding string `json:"positionEncoding,omitempty"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type ReferenceParams struct {
	TextDocumentPositionParams
	Context struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}
