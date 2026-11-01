package app

import "encoding/json"

// AITool describes an action that the model may request. The model never gets
// direct access to the implementation; Service remains the execution gate.
type AITool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// commandToolRegistry is deliberately small. New capabilities should be added
// here before they are exposed to a local model.
func commandToolRegistry() []AITool {
	return []AITool{
		{
			Name: "ssh.exec",
			Description: "Execute one command in the selected active SSH session. The command is always subject to Eiksy Command Policy and may require user approval.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sessionId": map[string]any{"type": "string", "description": "Active Eiksy SSH session ID."},
					"command":   map[string]any{"type": "string", "description": "One shell command to execute. Do not combine commands with shell operators."},
					"reason":    map[string]any{"type": "string", "description": "Short explanation of why the command is needed."},
				},
				"required": []string{"sessionId", "command"},
				"additionalProperties": false,
			},
		},
		{
			Name: "sftp.write",
			Description: "Write UTF-8 content to a file in the selected active SSH session through SFTP. Always requires explicit user approval; request size is bounded.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sessionId": map[string]any{"type": "string", "description": "Active Eiksy SSH session ID."},
					"path":      map[string]any{"type": "string", "description": "Remote file path to overwrite or create."},
					"content":   map[string]any{"type": "string", "description": "Complete UTF-8 file content to write."},
					"reason":    map[string]any{"type": "string", "description": "Short explanation of why the file needs to be changed."},
				},
				"required": []string{"sessionId", "path", "content"},
				"additionalProperties": false,
			},
		},
		{
			Name: "sftp.stat",
			Description: "Read metadata for one file or directory in the selected active SSH session through SFTP. Read-only; no file content is returned.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sessionId": map[string]any{"type": "string", "description": "Active Eiksy SSH session ID."},
					"path": map[string]any{"type": "string", "description": "Remote file or directory path."},
				},
				"required": []string{"sessionId", "path"},
				"additionalProperties": false,
			},
		},
		{
			Name: "sftp.read",
			Description: "Read a bounded amount of UTF-8 text from a file in the selected active SSH session through SFTP. Read-only; use only when file content is explicitly needed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sessionId": map[string]any{"type": "string", "description": "Active Eiksy SSH session ID."},
					"path":      map[string]any{"type": "string", "description": "Remote file path to read."},
				},
				"required": []string{"sessionId", "path"},
				"additionalProperties": false,
			},
		},
		{
			Name: "sftp.list",
			Description: "List files and directories in the selected active SSH session through SFTP. Read-only; the path is requested by the model and the result is bounded.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sessionId": map[string]any{"type": "string", "description": "Active Eiksy SSH session ID."},
					"path":      map[string]any{"type": "string", "description": "Remote directory path to list. Use . for the current directory."},
				},
				"required": []string{"sessionId", "path"},
				"additionalProperties": false,
			},
		},
		{
			Name: "ssh.diagnostics",
			Description: "Run a fixed, read-only infrastructure diagnostic summary against the selected active SSH session. No arbitrary command is accepted.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sessionId": map[string]any{"type": "string", "description": "Active Eiksy SSH session ID."},
					"operation": map[string]any{"type": "string", "enum": []string{"summary"}, "description": "Diagnostic operation to run."},
				},
				"required": []string{"sessionId", "operation"},
				"additionalProperties": false,
			},
		},
	}
}

// localAgentToolRegistry exposes only local-host capabilities. It intentionally
// contains no session identifiers and no SSH/SFTP operations.
func localAgentToolRegistry() []AITool {
	return []AITool{
		{Name: "local.exec", Description: "Execute one command on the local host as the current operating-system user. Command Policy applies and approval may be required.", Parameters: map[string]any{
			"type":"object", "properties":map[string]any{
				"command":map[string]any{"type":"string","description":"One local shell command. Do not combine commands with shell operators."},
				"reason":map[string]any{"type":"string","description":"Why this command is needed."},
			}, "required":[]string{"command"}, "additionalProperties":false,
		}},
		{Name: "local.file.read", Description: "Read a bounded UTF-8 text file from the local host. Subject to local-files policy.", Parameters: map[string]any{
			"type":"object", "properties":map[string]any{"path":map[string]any{"type":"string","description":"Path relative to the CLI working directory or an absolute path."}}, "required":[]string{"path"}, "additionalProperties":false,
		}},
		{Name: "local.file.write", Description: "Create or overwrite a local file with UTF-8 content. This operation requires user approval unless an explicit local policy rule allows it.", Parameters: map[string]any{
			"type":"object", "properties":map[string]any{"path":map[string]any{"type":"string"},"content":map[string]any{"type":"string"},"reason":map[string]any{"type":"string"}}, "required":[]string{"path","content"}, "additionalProperties":false,
		}},
		{Name: "local.file.list", Description: "List a local directory, bounded to 256 entries. Subject to local-files policy.", Parameters: map[string]any{
			"type":"object", "properties":map[string]any{"path":map[string]any{"type":"string","description":"Directory path relative to the CLI working directory or an absolute path."}}, "required":[]string{"path"}, "additionalProperties":false,
		}},
	}
}

func openAILocalToolDefinitions() []map[string]any {
	registry := localAgentToolRegistry()
	tools := make([]map[string]any, 0, len(registry))
	for _, tool := range registry {
		tools = append(tools, map[string]any{"type":"function","function":map[string]any{
			"name":tool.Name,"description":tool.Description,"parameters":tool.Parameters,
		}})
	}
	return tools
}

// openAIToolDefinitions converts the internal registry into the OpenAI
// compatible tools representation used by Ollama/llama-server and compatible
// local providers.
func openAIToolDefinitions() []map[string]any {
	registry := commandToolRegistry()
	tools := make([]map[string]any, 0, len(registry))
	for _, tool := range registry {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.Parameters,
			},
		})
	}
	return tools
}

func marshalAITools() ([]byte, error) {
	return json.Marshal(openAIToolDefinitions())
}
