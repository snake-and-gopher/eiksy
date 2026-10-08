package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"eiksy/internal/domain/ai"
	"eiksy/internal/securestorage"
)

const (
	maxAIModelDownloadSize   int64 = 16 << 30
	maxAIModelListResponseSize int64 = 1 << 20
	maxVaultResponseSize       int64 = 1 << 20
)

func (s *Service) SelectAIProvider(providerID string) error {
	state := s.store.AIState()
	index := providerIndexByID(state.Providers, providerID)
	if index < 0 {
		return fmt.Errorf("ai provider %q not found", providerID)
	}

	for i := range state.Providers {
		state.Providers[i].Selected = state.Providers[i].ID == providerID
	}

	if err := s.store.UpdateAIState(state); err != nil {
		return fmt.Errorf("persist AI provider selection: %w", err)
	}
	s.EmitLog("info", fmt.Sprintf("Switched AI provider to %s.", state.Providers[index].Name))
	return nil
}

func (s *Service) SaveCloudProvider(model, endpoint, token string) error {
	model = strings.TrimSpace(model)
	endpoint = strings.TrimSpace(endpoint)
	token = strings.TrimSpace(token)
	if model == "" {
		return fmt.Errorf("model name is required")
	}
	if endpoint == "" {
		return fmt.Errorf("cloud endpoint is required")
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("cloud endpoint must be a valid http or https url")
	}

	state := s.store.AIState()
	index := providerIndexByClass(state.Providers, ai.ProviderClassOpenAICompatible)
	if index < 0 {
		return fmt.Errorf("cloud ai provider is not available")
	}

	for i := range state.Providers {
		state.Providers[i].Selected = i == index
	}

	if token == "" {
		existingToken, err := s.store.LoadSecret(securestorage.AIProviderTokenKey(state.Providers[index].ID))
		switch {
		case err == nil && strings.TrimSpace(existingToken) != "":
			token = strings.TrimSpace(existingToken)
		case err != nil && !errors.Is(err, securestorage.ErrMasterPasswordRequired):
			return err
		case strings.TrimSpace(state.Providers[index].Token) != "":
			token = strings.TrimSpace(state.Providers[index].Token)
		}
	}
	if token != "" {
		if err := s.store.StoreSecret(securestorage.AIProviderTokenKey(state.Providers[index].ID), token); err != nil {
			return err
		}
	}
	state.Providers[index].Model = model
	state.Providers[index].Endpoint = endpoint
	state.Providers[index].Token = ""
	state.Providers[index].HasToken = s.store.SecretExists(securestorage.AIProviderTokenKey(state.Providers[index].ID))
	state.Providers[index].Status = "ready"
	state.Providers[index].Configured = true
	if err := s.store.UpdateAIState(state); err != nil {
		return fmt.Errorf("persist cloud AI provider: %w", err)
	}
	s.EmitLog("info", fmt.Sprintf("Saved cloud AI provider %s (%s).", state.Providers[index].Name, model))
	return nil
}

func (s *Service) SaveLocalProvider(downloadURL string) error {
	downloadURL = strings.TrimSpace(downloadURL)
	if downloadURL == "" {
		return fmt.Errorf("model url is required")
	}

	parsed, err := url.Parse(downloadURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("model url must be a valid http or https url")
	}

	state := s.store.AIState()
	index := providerIndexByID(state.Providers, localAIProviderID)
	if index < 0 {
		return fmt.Errorf("local ai provider is not available")
	}

	for i := range state.Providers {
		state.Providers[i].Selected = i == index
	}
	state.Providers[index].DownloadURL = downloadURL
	state.Providers[index].Endpoint = localAIEndpoint
	state.Providers[index].Model = localModelNameFromURL(downloadURL)
	if state.Providers[index].LocalPath != "" && fileExists(state.Providers[index].LocalPath) {
		state.Providers[index].Configured = true
		state.Providers[index].Status = "stopped"
	} else {
		state.Providers[index].Configured = false
		state.Providers[index].LocalPath = ""
		state.Providers[index].Status = "download required"
	}
	if err := s.store.UpdateAIState(state); err != nil {
		return fmt.Errorf("persist local AI provider: %w", err)
	}
	s.EmitLog("info", "Saved local AI model URL.")
	return nil
}

func (s *Service) DownloadLocalModel(downloadURL string) error {
	downloadURL = strings.TrimSpace(downloadURL)
	if downloadURL == "" {
		return fmt.Errorf("model url is required")
	}
	parsed, err := url.Parse(downloadURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("model url must be a valid http or https url")
	}

	state := s.store.AIState()
	index := providerIndexByID(state.Providers, localAIProviderID)
	if index < 0 {
		return fmt.Errorf("local ai provider is not available")
	}

	modelsDir, err := localModelsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		return fmt.Errorf("create local models directory: %w", err)
	}

	filename := localModelFilenameFromURL(downloadURL)
	targetPath := filepath.Join(modelsDir, filename)
	tempPath := targetPath + ".part"

	req, err := http.NewRequestWithContext(s.resolveContext(nil), http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("model download returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	if resp.ContentLength > maxAIModelDownloadSize {
		return fmt.Errorf("model download exceeds %d bytes", maxAIModelDownloadSize)
	}

	file, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("create model file: %w", err)
	}
	limitedBody := io.LimitReader(resp.Body, maxAIModelDownloadSize+1)
	written, err := io.Copy(file, limitedBody)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("write model file: %w", err)
	}
	if written > maxAIModelDownloadSize {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("model download exceeds %d bytes", maxAIModelDownloadSize)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close model file: %w", err)
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("finalize model file: %w", err)
	}

	state = s.store.AIState()
	index = providerIndexByID(state.Providers, localAIProviderID)
	if index < 0 {
		return fmt.Errorf("local ai provider is not available")
	}
	for i := range state.Providers {
		state.Providers[i].Selected = i == index
	}
	state.Providers[index].LocalPath = targetPath
	state.Providers[index].Endpoint = localAIEndpoint
	state.Providers[index].DownloadURL = downloadURL
	state.Providers[index].Model = localModelNameFromURL(downloadURL)
	state.Providers[index].Configured = true
	state.Providers[index].Status = "stopped"
	if err := s.store.UpdateAIState(state); err != nil {
		return fmt.Errorf("persist downloaded local AI model state: %w", err)
	}
	s.EmitLog("info", fmt.Sprintf("Downloaded local AI model to %s.", targetPath))
	return nil
}

func (s *Service) ListCloudModels(endpoint, token string) ([]string, error) {
	endpoint = strings.TrimSpace(endpoint)
	token = strings.TrimSpace(token)
	if endpoint == "" {
		return nil, fmt.Errorf("cloud endpoint is required")
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("cloud endpoint must be a valid http or https url")
	}

	if token == "" {
		state := s.store.AIState()
		if index := providerIndexByClass(state.Providers, ai.ProviderClassOpenAICompatible); index >= 0 {
			token, err = s.store.LoadSecret(securestorage.AIProviderTokenKey(state.Providers[index].ID))
			if err != nil && !errors.Is(err, securestorage.ErrMasterPasswordRequired) {
				return nil, err
			}
			token = strings.TrimSpace(token)
			if token == "" {
				token = strings.TrimSpace(state.Providers[index].Token)
			}
		}
	}

	req, err := http.NewRequestWithContext(s.resolveContext(nil), http.MethodGet, strings.TrimRight(endpoint, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAIModelListResponseSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(respBody)) > maxAIModelListResponseSize {
		return nil, fmt.Errorf("AI API model list response exceeds %d bytes", maxAIModelListResponseSize)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AI API returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("parse model list response: %w", err)
	}

	unique := map[string]struct{}{}
	models := make([]string, 0, len(payload.Data))
	for _, entry := range payload.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, exists := unique[id]; exists {
			continue
		}
		unique[id] = struct{}{}
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}
func (s *Service) StartLocalModel() error {
	state := s.store.AIState()
	index := providerIndexByID(state.Providers, localAIProviderID)
	if index < 0 {
		return fmt.Errorf("local ai provider is not available")
	}
	provider := state.Providers[index]
	if strings.TrimSpace(provider.LocalPath) == "" {
		return fmt.Errorf("download the local model first")
	}
	if !fileExists(provider.LocalPath) {
		return fmt.Errorf("local model file not found: %s", provider.LocalPath)
	}

	binaryPath, err := exec.LookPath("llama-server")
	if err != nil {
		return fmt.Errorf("llama-server is not installed or not in PATH")
	}

	s.localAIMu.Lock()
	if s.hasLiveLocalModelProcessLocked() {
		s.localAIMu.Unlock()
		return nil
	}
	s.localAIStopping = false
	probe, err := net.Listen("tcp", net.JoinHostPort(localAIHost, localAIPort))
	if err != nil {
		s.localAIMu.Unlock()
		return fmt.Errorf("local AI port %s is already in use", localAIPort)
	}
	_ = probe.Close()

	stderr := &bytes.Buffer{}
	done := make(chan error, 1)
	cmd := exec.CommandContext(s.resolveContext(nil), binaryPath,
		"--model", provider.LocalPath,
		"--host", localAIHost,
		"--port", localAIPort,
		"--jinja",
	)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		s.localAIMu.Unlock()
		return fmt.Errorf("start llama-server: %w", err)
	}
	s.localAICmd = cmd
	s.localAIDone = done
	s.localAIErr = stderr
	s.localAIMu.Unlock()
	go s.watchLocalModelProcess(cmd, done, stderr)

	if err := s.waitForLocalModelReady(localAIEndpoint, provider.Model, 30*time.Second); err != nil {
		_ = s.StopLocalModel()
		return s.localAIErrorWithDetails(err, stderr)
	}

	state = s.store.AIState()
	index = providerIndexByID(state.Providers, localAIProviderID)
	if index < 0 {
		return fmt.Errorf("local ai provider is not available")
	}
	for i := range state.Providers {
		state.Providers[i].Selected = i == index
	}
	state.Providers[index].Configured = true
	state.Providers[index].Status = "running"
	state.Providers[index].Endpoint = localAIEndpoint
	if err := s.store.UpdateAIState(state); err != nil {
		return fmt.Errorf("persist local AI model running state: %w", err)
	}
	s.EmitLog("info", "Local AI model started.")
	return nil
}

func (s *Service) StopLocalModel() error {
	s.localAIMu.Lock()
	cmd := s.localAICmd
	done := s.localAIDone
	s.localAICmd = nil
	s.localAIDone = nil
	s.localAIErr = nil
	s.localAIStopping = true
	s.localAIMu.Unlock()
	defer func() {
		s.localAIMu.Lock()
		s.localAIStopping = false
		s.localAIMu.Unlock()
	}()

	if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("stop llama-server: %w", err)
		}
		if done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				return fmt.Errorf("timeout waiting for llama-server to stop")
			}
		}
	}

	state := s.store.AIState()
	index := providerIndexByID(state.Providers, localAIProviderID)
	if index >= 0 {
		if state.Providers[index].LocalPath != "" && fileExists(state.Providers[index].LocalPath) {
			state.Providers[index].Configured = true
			state.Providers[index].Status = "stopped"
		} else {
			state.Providers[index].Configured = false
			state.Providers[index].Status = "download required"
		}
		if err := s.store.UpdateAIState(state); err != nil {
			return fmt.Errorf("persist local AI model stopped state: %w", err)
		}
	}
	s.EmitLog("info", "Local AI model stopped.")
	return nil
}

func providerIndexByID(providers []ai.ProviderDescriptor, providerID string) int {
	for index, provider := range providers {
		if provider.ID == providerID {
			return index
		}
	}

	return -1
}

func providerIndexByClass(providers []ai.ProviderDescriptor, class ai.ProviderClass) int {
	for index, provider := range providers {
		if provider.Class == class {
			return index
		}
	}

	return -1
}

func (s *Service) isLocalModelRunning() bool {
	s.localAIMu.Lock()
	defer s.localAIMu.Unlock()
	return s.hasLiveLocalModelProcessLocked()
}

func (s *Service) watchLocalModelProcess(cmd *exec.Cmd, done chan error, stderr *bytes.Buffer) {
	err := cmd.Wait()
	done <- err
	close(done)
	s.localAIMu.Lock()
	wasActive := s.localAICmd == cmd
	stopping := s.localAIStopping
	if wasActive {
		s.localAICmd = nil
		s.localAIDone = nil
		s.localAIErr = nil
	}
	s.localAIMu.Unlock()

	state := s.store.AIState()
	index := providerIndexByID(state.Providers, localAIProviderID)
	if index >= 0 {
		if state.Providers[index].LocalPath != "" && fileExists(state.Providers[index].LocalPath) {
			state.Providers[index].Configured = true
			state.Providers[index].Status = "stopped"
		} else {
			state.Providers[index].Configured = false
			state.Providers[index].Status = "download required"
		}
		if err := s.store.UpdateAIState(state); err != nil {
			s.EmitLog("error", fmt.Sprintf("persist local AI process state: %v", err))
		}
	}
	if wasActive && !stopping && err != nil && !errors.Is(err, os.ErrProcessDone) {
		s.EmitLog("warn", s.localAIErrorWithDetails(fmt.Errorf("local AI model stopped: %w", err), stderr).Error())
	}
}

func (s *Service) waitForLocalModelReady(endpoint, model string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		models, err := s.ListCloudModels(endpoint, "")
		if err == nil {
			if len(models) == 0 || model == "" || slices.Contains(models, model) {
				return nil
			}
			lastErr = fmt.Errorf("model %q is not reported by the local server yet", model)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout waiting for local model server")
	}
	return fmt.Errorf("local model did not become ready: %w", lastErr)
}

func (s *Service) localAIErrorWithDetails(err error, stderr *bytes.Buffer) error {
	if stderr == nil {
		return err
	}
	details := strings.TrimSpace(stderr.String())
	if details == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, details)
}

func (s *Service) hasLiveLocalModelProcessLocked() bool {
	if s.localAICmd == nil || s.localAICmd.Process == nil {
		return false
	}
	if s.localAIDone != nil {
		select {
		case <-s.localAIDone:
			s.localAICmd = nil
			s.localAIDone = nil
			s.localAIErr = nil
			return false
		default:
		}
	}
	return true
}

func localModelsDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, ".eiksy", "models"), nil
}

func localModelFilenameFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err == nil {
		if name := path.Base(strings.TrimSpace(parsed.Path)); name != "." && name != "/" && name != "" {
			return name
		}
	}
	return "Qwen3-4B-Q4_K_M.gguf"
}

func localModelNameFromURL(rawURL string) string {
	filename := localModelFilenameFromURL(rawURL)
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

func fileExists(target string) bool {
	info, err := os.Stat(target)
	return err == nil && !info.IsDir()
}

// SendChatMessage adds the user message to the conversation, calls the
// configured AI provider, and appends the assistant reply.
func (s *Service) SendChatMessage(ctx context.Context, message string, activeSessionID string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("message cannot be empty")
	}
	activeSessionID = strings.TrimSpace(activeSessionID)
	state := s.store.AIState()
	provider, err := s.activeConfiguredProvider(state)
	if err != nil {
		return err
	}
	if provider == nil {
		return fmt.Errorf("no AI provider is configured; configure one in Settings first")
	}
	s.emitFn("ai:status", map[string]string{"status": "thinking"})
	defer s.emitFn("ai:status", map[string]string{"status": "idle"})
	_, _, err = s.sendChatMessageWithNativeTools(s.resolveContext(ctx), provider, state, activeSessionID, message)
	if err != nil {
		return fmt.Errorf("AI request failed: %w", err)
	}
	return nil
}

 // SendPlainChatMessage is the sidebar's provider-only chat path. It deliberately
 // omits session context, Eiksy system prompts, and tool schemas.
func (s *Service) SendPlainChatMessage(ctx context.Context, message string) error {
	message = strings.TrimSpace(message)
	if message == "" { return fmt.Errorf("message cannot be empty") }
	state := s.store.AIState()
	provider, err := s.activeConfiguredProvider(state)
	if err != nil { return err }
	if provider == nil { return fmt.Errorf("no AI provider is configured; configure one in Settings first") }
	messages := make([]nativeChatMessage, 0, len(state.Messages)+1)
	for _, previous := range state.Messages {
		if previous.Role == "user" || previous.Role == "assistant" {
			messages = append(messages, nativeChatMessage{Role: previous.Role, Content: previous.Content})
		}
	}
	messages = append(messages, nativeChatMessage{Role: "user", Content: message})
	s.emitFn("ai:status", map[string]string{"status": "thinking"})
	defer s.emitFn("ai:status", map[string]string{"status": "idle"})
	response, err := s.callNativeToolCompletion(s.resolveContext(ctx), provider, messages, "", nil)
	if err != nil { return fmt.Errorf("AI request failed: %w", err) }
	if len(response.ToolCalls) != 0 { return fmt.Errorf("plain chat provider returned unsupported tool calls") }
	reply := strings.TrimSpace(response.Content)
	if reply == "" { return fmt.Errorf("AI returned an empty response") }
	latest := s.store.AIState()
	latest.Messages = append(latest.Messages, ai.ChatMessage{Role: "user", Content: message}, ai.ChatMessage{Role: "assistant", Content: reply})
	if err := s.store.UpdateAIState(latest); err != nil { return fmt.Errorf("persist AI chat response: %w", err) }
	s.emitFn("ai:message", map[string]string{"role": "user", "content": message})
	s.emitFn("ai:message", map[string]string{"role": "assistant", "content": reply})
	return nil
}

func (s *Service) activeConfiguredProvider(state ai.WorkspaceState) (*ai.ProviderDescriptor, error) {
	for i := range state.Providers {
		if state.Providers[i].Selected && state.Providers[i].Configured {
			provider := state.Providers[i]
			if provider.Class == ai.ProviderClassOpenAICompatible {
				token, err := s.store.LoadSecret(securestorage.AIProviderTokenKey(provider.ID))
				if err != nil && !errors.Is(err, securestorage.ErrMasterPasswordRequired) {
					return nil, err
				}
				if strings.TrimSpace(token) != "" {
					provider.Token = strings.TrimSpace(token)
				}
				provider.HasToken = provider.Token != ""
			}
			if provider.Class == ai.ProviderClassLocalOpenAI {
				if strings.TrimSpace(provider.Endpoint) == "" {
					provider.Endpoint = localAIEndpoint
				}
				if strings.TrimSpace(provider.LocalPath) == "" || !fileExists(provider.LocalPath) {
					return nil, fmt.Errorf("local AI model is not downloaded; download it in Settings first")
				}
				if !s.isLocalModelRunning() {
					return nil, fmt.Errorf("local AI model is stopped; start it in Settings first")
				}
			}
			return &provider, nil
		}
	}
	return nil, nil
}
func (s *Service) scrubAIStateForShell(state ai.WorkspaceState) ai.WorkspaceState {
	scrubbed := state
	scrubbed.Providers = append([]ai.ProviderDescriptor(nil), state.Providers...)
	scrubbed.CommandPolicy = normalizeCommandPolicy(state.CommandPolicy)
	for i := range scrubbed.Providers {
		scrubbed.Providers[i].Token = ""
		scrubbed.Providers[i].HasToken = s.store.SecretExists(securestorage.AIProviderTokenKey(scrubbed.Providers[i].ID))
		if scrubbed.Providers[i].Class == ai.ProviderClassLocalOpenAI {
			hasModel := scrubbed.Providers[i].LocalPath != "" && fileExists(scrubbed.Providers[i].LocalPath)
			scrubbed.Providers[i].Configured = hasModel
			if strings.TrimSpace(scrubbed.Providers[i].Endpoint) == "" {
				scrubbed.Providers[i].Endpoint = localAIEndpoint
			}
			switch {
			case hasModel && s.isLocalModelRunning():
				scrubbed.Providers[i].Status = "running"
			case hasModel:
				scrubbed.Providers[i].Status = "stopped"
			default:
				scrubbed.Providers[i].Status = "download required"
			}
		}
	}
	scrubbed.Messages = append([]ai.ChatMessage{}, state.Messages...)
	scrubbed.ChatSessions = make([]ai.ChatSession, len(state.ChatSessions))
	for i, session := range state.ChatSessions { scrubbed.ChatSessions[i] = session; scrubbed.ChatSessions[i].Messages = nil }
	return scrubbed
}
