import "./style.css";
import "./app.css";
import "@xterm/xterm/css/xterm.css";
import appLogo from "./assets/images/logo.png";
import packageJson from "../package.json";
import appFavicon from "./assets/images/favicon.ico";

import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import {
  AcceptSSHHostKey,
  ClearChat,
  CloseSession,
  ConnectSSH,
  ReconnectSession,
  CreateSessionProfile,
  DownloadSFTPFiles,
  DownloadLocalModel,
  DeleteSessionProfile,
  DisconnectSSH,
  EnsureMasterPassword,
  GetCloudProviderAuthSession,
  GetReleaseVersion,
  GetSecureStorageStatus,
  GetShellState,
  LaunchSession,
  ListSFTPFiles,
  NavigateSFTP,
  ImportSSHConfig,
  ReadSFTPFile,
  ListCloudModels,
  SaveCloudProvider,
  ResolveCommandPolicyRequest,
  SaveLocalProvider,
  SaveSFTPFile,
  SelectDownloadDirectory,
  SelectAIProvider,
  SelectUploadFiles,
  SendPlainChatMessage,
  SendSSHInput,
  ResizeTerminal,
  StartLocalModel,
  StartCloudProviderAuth,
  StopLocalModel,
  UploadSFTPFiles,
  UpdateCommandPolicy,
  UpdateSettings,
} from "../wailsjs/go/main/App";
import { BrowserOpenURL, EventsOn } from "../wailsjs/runtime/runtime";
import type {
  ai as aiModels,
  app as appModels,
  securestorage as securestorageModels,
  sessions,
  settings as settingsModels,
  sftp as sftpModels,
} from "../wailsjs/go/models";

type ShellState = appModels.ShellState;
type CloudProviderAuthSession = appModels.CloudProviderAuthSession;
type RuntimeSession = appModels.RuntimeSessionView;
type SessionProfile = sessions.ProfileInput;
type AIProvider = aiModels.ProviderDescriptor;
type CommandPolicyState = {
  tools: aiModels.CommandTool[];
  commandRules: aiModels.CommandRule[];
  pendingRequests: aiModels.CommandRequest[];
  localDocsPath?: string;
};
type FileEntry = sftpModels.FileEntry;
type SecureStorageStatus = securestorageModels.Status;

type SessionFormState = {
  name: string;
  host: string;
  port: string;
  username: string;
  password: string;
  keyPassphrase: string;
  authMethod: "password" | "key";
  privateKeyPath: string;
  protocolId: string;
  tags: string[];
  proxyJump: string;
  localForwards: string;
  useSSHAgent: boolean;
  hasSavedPassword: boolean;
  hasSavedKeyPassphrase: boolean;
};

type SFTPState = {
  tabId: string | null;
  path: string;
  entries: FileEntry[];
  loading: boolean;
  error: string;
  editorOpen: boolean;
  editorPath: string;
  editorContent: string;
  editorLoading: boolean;
  editorSaving: boolean;
  editorDirty: boolean;
  editorError: string;
  selectedFiles: string[];
};

type TerminalState = {
  terminal: Terminal;
  fitAddon: FitAddon;
  wrapper: HTMLDivElement;
  inner: HTMLDivElement;
  opened: boolean;
  unsubscribe: (() => void) | null;
};

type Theme = "dark" | "light" | "green";
type TerminalTheme = "black-on-white" | "gray-on-black" | "green-on-black";
type SettingsTab =
  "ai" | "commandpolicy" | "sshconfig" | "portforward" | "theme" | "about";
type SessionModalTab = "host" | "auth" | "network" | "other";
type SessionInnerTab = "console" | "sftp";
type VaultAuthMethod = "token" | "oidc" | "oidc-sec" | "domain";
type CommandPolicyTab = "access" | "tools" | "settings";

type PortForwardRule = {
  localPort: string;
  remoteHost: string;
  remotePort: string;
  hostId: string;
  enabled: boolean;
};

type NotificationLevel = "info" | "warn" | "error" | "debug";

type NotificationItem = {
  id: string;
  level: NotificationLevel;
  message: string;
  time: string;
};

type AIActivity = {
  id: string;
  status: string;
  sessionId: string;
  command: string;
  approval: string;
  exitCode: number;
  durationMs: number;
  stdout: string;
  stderr: string;
  errorType: string;
  error: string;
  message: string;
  time: string;
};

const AI_ACTIVITY_HISTORY_KEY = "eiksy.ai-operate-history";
const MAX_AI_ACTIVITY_HISTORY = 20;

type MasterPasswordDialogMode = "create" | "unlock";

type MasterPasswordDialogState = {
  visible: boolean;
  mode: MasterPasswordDialogMode;
  password: string;
  confirm: string;
  error: string;
  reason: string;
  required: boolean;
  resolver: ((completed: boolean) => void) | null;
};

type SessionContextMenuState = {
  visible: boolean;
  x: number;
  y: number;
  profileId: string;
};

type HostKeyDialogState = {
  visible: boolean;
  tabId: string;
  profileId: string;
  fingerprint: string;
  hostname: string;
};

const THEME_KEY = "eiksy-theme";
const TERMINAL_THEME_KEY = "eiksy-terminal-theme";
const SIDEBAR_COLLAPSED_KEY = "eiksy-sidebar-collapsed";
const ASSISTANT_COLLAPSED_KEY = "eiksy-assistant-collapsed";
const SESSION_INNER_TABS_KEY = "eiksy-session-inner-tabs";
const THEMES: Theme[] = ["dark", "light", "green"];
const TERMINAL_THEMES: TerminalTheme[] = ["black-on-white", "gray-on-black", "green-on-black"];
const APP_METADATA = {
  name: "Eiksy",
  repositoryUrl: "https://github.com/burenk0v/eiksy",
  latestReleaseUrl: "https://github.com/burenk0v/eiksy/releases/latest",
  copyright: "Eiksy contributors",
  license: "Apache License 2.0",
  legalNotice: "Distributed under the Apache License 2.0.",
};

function isTheme(value: string | null | undefined): value is Theme {
  return !!value && THEMES.includes(value as Theme);
}

function isTerminalTheme(value: string | null | undefined): value is TerminalTheme {
  return !!value && TERMINAL_THEMES.includes(value as TerminalTheme);
}

const root = document.querySelector<HTMLDivElement>("#app");

class EiksyShell {
  private readonly sidebarActionsMenuID = "sidebar-actions-menu";
  private readonly untaggedFilterTag = "__untagged__";
  private shellState: ShellState | null = null;
  private activeTabId = "";
  private errorMessage = "";
  private startupComplete = false;
  private showSessionModal = false;
  private sessionModalTab: SessionModalTab = "host";
  private editingProfileID = "";
  private sessionNameAuto = true;
  private showSettingsModal = false;
  private settingsTab: SettingsTab = "ai";
  private commandPolicyTab: CommandPolicyTab = "access";
  private sessionInnerTab: SessionInnerTab = "console";
  private sessionForm: SessionFormState = this.defaultSessionForm();
  private sidebarCollapsed = false;
  private assistantCollapsed = false;
  private sessionInnerTabs = new Map<string, SessionInnerTab>();
  private terminals = new Map<string, TerminalState>();
  private sftpState: SFTPState = {
    tabId: null,
    path: "",
    entries: [],
    loading: false,
    error: "",
    editorOpen: false,
    editorPath: "",
    editorContent: "",
    editorLoading: false,
    editorSaving: false,
    editorDirty: false,
    editorError: "",
    selectedFiles: [],
  };
  private sshConfigDraft = "";
  private theme: Theme;
  private terminalTheme: TerminalTheme;
  private sessionContextMenu: SessionContextMenuState = {
    visible: false,
    x: 0,
    y: 0,
    profileId: "",
  };
  private hostKeyDialog: HostKeyDialogState = {
    visible: false,
    tabId: "",
    profileId: "",
    fingerprint: "",
    hostname: "",
  };
  private masterPasswordDialog: MasterPasswordDialogState = {
    visible: false,
    mode: "create",
    password: "",
    confirm: "",
    error: "",
    reason: "",
    required: false,
    resolver: null,
  };
  private startupMasterPasswordPrompted = false;
  private selectedSessionTags = new Set<string>();
  private knownSessionTags = new Set<string>();
  private sessionTagFilterInitialized = false;
  private sessionTagDraft = "";
  private sessionTagInputVisible = false;
  private chatDraftMessage = "";
  private aiStatus: "idle" | "thinking" = "idle";
  private cloudModels: string[] = [];
  private cloudModelsEndpoint = "";
  private cloudModelsLoading = false;
  private cloudModelsError = "";
  private pfNewLocalPort = "";
  private pfNewRemoteHost = "";
  private pfNewRemotePort = "";
  private pfNewHostId = "";
  private cloudDraftModel = "";
  private cloudDraftEndpoint = "";
  private cloudDraftToken = "";
  private localDraftDownloadURL = "";
  private cloudAuthSessionId = "";
  private cloudAuthPending = false;
  private cloudAuthMessage = "";
  private cloudAuthPollTimer: number | null = null;
  private appVersion = packageJson.version;
  private vaultDraftAddress = "";
  private vaultDraftMountPoint = "";
  private vaultDraftAuthMethod: VaultAuthMethod = "token";
  private vaultDraftLogin = "";
  private vaultDraftToken = "";
  private vaultDraftPassword = "";
  private vaultDraftAutoRenewToken = false;
  private vaultDraftKeePassDatabasePath = "";
  private vaultDraftKeePassPassword = "";
  private notifications: NotificationItem[] = [];
  private aiActivities: AIActivity[] = [];
  private toastQueue: NotificationItem[] = [];
  private showNotificationCenter = false;
  private showSidebarActionsMenu = false;

  constructor() {
    const saved = localStorage.getItem(THEME_KEY);
    this.theme = isTheme(saved) ? saved : "dark";
    const savedTerminalTheme = localStorage.getItem(TERMINAL_THEME_KEY);
    this.terminalTheme = isTerminalTheme(savedTerminalTheme) ? savedTerminalTheme : "gray-on-black";
    this.sidebarCollapsed =
      localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "true";
    this.assistantCollapsed =
      localStorage.getItem(ASSISTANT_COLLAPSED_KEY) === "true";
    this.sessionInnerTabs = this.loadStoredSessionInnerTabs();
    this.applyTheme();
    this.applyFavicon();
  }

  private applyTheme(): void {
    document.documentElement.classList.remove("light", "green");
    if (this.theme !== "dark") {
      document.documentElement.classList.add(this.theme);
    }
    localStorage.setItem(THEME_KEY, this.theme);
  }

  private terminalThemeOptions(): Terminal["options"]["theme"] {
    switch (this.terminalTheme) {
      case "black-on-white":
        return { background: "#ffffff", foreground: "#000000", cursor: "#000000", selectionBackground: "#b8c7dc" };
      case "green-on-black":
        return { background: "#000000", foreground: "#00ff66", cursor: "#00ff66", selectionBackground: "#145c36" };
      case "gray-on-black":
      default:
        return { background: "#000000", foreground: "#b8b8b8", cursor: "#b8b8b8", selectionBackground: "#3a3a3a" };
    }
  }

  private applyTerminalTheme(): void {
    const theme = this.terminalThemeOptions();
    for (const state of this.terminals.values()) state.terminal.options.theme = theme;
    localStorage.setItem(TERMINAL_THEME_KEY, this.terminalTheme);
  }

  private setTerminalTheme(theme: TerminalTheme): void {
    if (this.terminalTheme === theme) return;
    this.terminalTheme = theme;
    this.applyTerminalTheme();
    this.render();
  }

  private applyFavicon(): void {
    const existing =
      document.querySelector<HTMLLinkElement>("link[rel='icon']");
    if (existing) {
      existing.href = appFavicon;
      return;
    }

    const link = document.createElement("link");
    link.rel = "icon";
    link.type = "image/x-icon";
    link.href = appFavicon;
    document.head.appendChild(link);
  }

  async bootstrap(): Promise<void> {
    if (!root) {
      return;
    }

    this.renderStartupState("Starting Eiksy…");
    this.installStartupDiagnostics();
    this.loadAIActivityHistory();
    this.registerGlobalEvents();

    try {
      await this.refreshAppVersion();
      await this.refresh();
      this.startupComplete = true;
      void this.promptForMasterPasswordOnStartup();
      window.addEventListener("resize", () => {
        this.fitActiveTerminal();
        if (this.showSidebarActionsMenu || this.sessionContextMenu.visible) {
          this.positionFloatingMenus();
        }
      });
    } catch (error) {
      const message = formatError("Eiksy could not start", error);
      this.errorMessage = message;
      this.renderStartupState(message, true);
      console.error(message, error);
    }
  }

  private installStartupDiagnostics(): void {
    window.addEventListener("error", (event) => {
      const message = event.error instanceof Error ? event.error.message : event.message;
      if (!message) return;
      this.errorMessage = `Frontend error: ${message}`;
      if (this.startupComplete) {
        this.setErrorMessage(this.errorMessage);
        return;
      }
      this.renderStartupState(this.errorMessage, true);
      console.error("Eiksy frontend error", event.error ?? event.message);
    });

    window.addEventListener("unhandledrejection", (event) => {
      const message = formatError(
        this.startupComplete ? "Unhandled UI error" : "Frontend startup error",
        event.reason,
      );
      this.errorMessage = message;
      if (this.startupComplete) {
        this.setErrorMessage(message);
        return;
      }
      this.renderStartupState(message, true);
      console.error("Eiksy unhandled rejection", event.reason);
    });
  }

  private renderStartupState(message: string, failed = false): void {
    if (!root) return;
    root.innerHTML = `
      <div class="startup-state">
        <div class="startup-state-title">Eiksy</div>
        <div class="startup-state-message">${escapeHtml(message)}</div>
        ${failed ? `
          <div class="startup-state-hint">
            Check the application logs and restart Eiksy. If the problem persists on Windows, verify that the WebView2 Runtime is installed.
          </div>
        ` : ""}
      </div>
    `;
  }

  private async refreshAppVersion(): Promise<void> {
    try {
      const version = await GetReleaseVersion();
      if (version && version.trim().length > 0) {
        this.appVersion = version.trim();
      }
    } catch {
      this.appVersion = packageJson.version;
    }
  }

  private registerGlobalEvents(): void {
    EventsOn("app:log", (...payload: unknown[]) => {
      const data = payload[0] as
        { level?: string; message?: string; time?: string } | undefined;
      if (!data?.message) return;
      const level = this.normalizeNotificationLevel(data.level);
      this.pushNotification(
        level,
        data.message,
        data.time ?? new Date().toISOString(),
      );
    });
    EventsOn("ai:status", (...payload: unknown[]) => {
      const data = payload[0] as { status?: string } | undefined;
      this.aiStatus = data?.status === "thinking" ? "thinking" : "idle";
      this.render();
    });
    EventsOn("ai:message", () => {
      void this.refresh("");
    });
    EventsOn("ai:operate", (...payload: unknown[]) => {
      const data = payload[0] as Partial<AIActivity> | undefined;
      if (!data) return;
      const activity: AIActivity = {
        id: crypto.randomUUID(),
        status: String(data.status ?? "unknown"),
        sessionId: String(data.sessionId ?? ""),
        command: String(data.command ?? ""),
        approval: String(data.approval ?? ""),
        exitCode: Number(data.exitCode ?? -1),
        durationMs: Number(data.durationMs ?? 0),
        stdout: String(data.stdout ?? ""),
        stderr: String(data.stderr ?? ""),
        errorType: String(data.errorType ?? ""),
        error: String(data.error ?? ""),
        message: String(data.message ?? ""),
        time: new Date().toISOString(),
      };
      this.aiActivities = [...this.aiActivities.slice(-19), activity].map(
        (entry) => ({
          ...entry,
          stdout: "",
          stderr: "",
        }),
      );
      this.persistAIActivityHistory();
      this.render();
    });
  }

  private loadAIActivityHistory(): void {
    try {
      const raw = localStorage.getItem(AI_ACTIVITY_HISTORY_KEY);
      if (!raw) {
        this.aiActivities = [];
        return;
      }
      const parsed = JSON.parse(raw);
      if (!Array.isArray(parsed)) {
        this.aiActivities = [];
        return;
      }
      this.aiActivities = parsed
        .filter((entry): entry is AIActivity => entry && typeof entry === "object")
        .slice(-MAX_AI_ACTIVITY_HISTORY)
        .map((entry) => ({
          id: String(entry.id ?? crypto.randomUUID()),
          status: String(entry.status ?? "unknown"),
          sessionId: String(entry.sessionId ?? ""),
          command: String(entry.command ?? ""),
          approval: String(entry.approval ?? ""),
          exitCode: Number(entry.exitCode ?? -1),
          durationMs: Number(entry.durationMs ?? 0),
          stdout: "",
          stderr: "",
          errorType: String(entry.errorType ?? ""),
          error: String(entry.error ?? ""),
          message: String(entry.message ?? ""),
          time: String(entry.time ?? new Date().toISOString()),
        }));
    } catch {
      this.aiActivities = [];
    }
  }

  private persistAIActivityHistory(): void {
    try {
      const safeHistory = this.aiActivities.slice(-MAX_AI_ACTIVITY_HISTORY).map((entry) => ({
        id: entry.id,
        status: entry.status,
        sessionId: entry.sessionId,
        command: entry.command,
        approval: entry.approval,
        exitCode: entry.exitCode,
        durationMs: entry.durationMs,
        errorType: entry.errorType,
        error: entry.error,
        message: entry.message,
        time: entry.time,
      }));
      localStorage.setItem(AI_ACTIVITY_HISTORY_KEY, JSON.stringify(safeHistory));
    } catch {
      // Local storage is optional; live activity remains available.
    }
  }

  private async refresh(errorMessage = this.errorMessage): Promise<void> {
    this.errorMessage = errorMessage;
    const state = await GetShellState();

    // Wails serializes nil Go slices as null. Normalize collection fields at
    // the UI boundary so rendering code can safely use array methods.
    state.protocols ??= [];
    state.sessionProfiles ??= [];
    state.sessionProfiles.forEach((profile) => {
      profile.tags ??= [];
    });
    state.activeSessions ??= [];
    state.sessionHistory ??= [];
    state.workspace ??= {} as ShellState["workspace"];
    state.workspace.layout ??= {} as ShellState["workspace"]["layout"];
    state.workspace.recentEvents ??= [];
    state.ai ??= {} as ShellState["ai"];
    state.ai.providers ??= [];
    state.ai.messages ??= [];
    state.ai.commandPolicy ??= {} as ShellState["ai"]["commandPolicy"];
    state.ai.commandPolicy.tools ??= [];
    state.ai.commandPolicy.commandRules ??= [];
    state.ai.commandPolicy.pendingRequests ??= [];

    this.shellState = state;
    this.reconcileSelectedSessionTags();
    this.pruneStoredSessionInnerTabs();
    const preferredTabID =
      this.activeTabId || this.shellState.workspace.layout.activeTabId || "";
    this.activeTabId = this.pickActiveTabID(preferredTabID);
    const activeTab = this.activeTab();
    this.sessionInnerTab = activeTab
      ? this.restoreSessionInnerTab(activeTab)
      : "console";
    if (this.sftpState.tabId !== this.activeTabId) {
      this.sftpState = this.defaultSFTPState(this.activeTabId || null);
    }
    this.render();
  }

  private closeRemoteEditor(): void {
    this.sftpState = {
      ...this.sftpState,
      editorOpen: false,
      editorPath: "",
      editorContent: "",
      editorLoading: false,
      editorSaving: false,
      editorDirty: false,
      editorError: "",
    };
    this.render();
  }

  private render(): void {
    if (!root || !this.shellState) {
      return;
    }

    root.innerHTML = `
            <div class="shell-root">
                <div class="shell ${this.sidebarCollapsed ? "sidebar-collapsed" : ""} ${this.assistantCollapsed ? "assistant-collapsed" : ""}">
                    ${this.renderSidebarPanel()}

                    <main class="panel workspace-panel">
                        <div class="tab-bar">${this.renderTabs()}</div>
                        ${this.errorMessage ? `<div class="error-banner">${escapeHtml(this.errorMessage)}</div>` : ""}
                        ${
                          this.activeTab()
                            ? `
                        <div class="session-inner-tabs">
                            ${this.renderSessionInnerTabs()}
                        </div>
                        `
                            : ""
                        }
                        ${this.activeTab() ? `
                        <div class="terminal-shell ${this.sessionInnerTab === "console" ? "" : "hidden"}">
                            <div id="terminal-host" class="terminal-container"></div>
                        </div>
                        <div class="sftp-workspace ${this.sessionInnerTab === "sftp" ? "" : "hidden"}">
                            <div class="sftp-workspace-header">
                                <div class="section-actions">
                                    ${this.renderSFTPActions()}
                                </div>
                            </div>
                            ${this.renderSFTPBrowser()}
                        </div>
                    ` : `
                        <div class="empty-workspace">
                            <div class="empty-workspace-card">
                                <div class="empty-workspace-title">No active session</div>
                                <div class="empty-workspace-message">Select a saved session from the sidebar to open a terminal.</div>
                            </div>
                        </div>
                    `}
                    </main>

                    ${this.renderAssistantPanel()}
                </div>
            </div>
            ${this.renderSessionContextMenu()}
            ${this.hostKeyDialog.visible ? this.renderHostKeyDialog() : ""}
            ${this.renderRemoteEditorModal()}
            ${this.showSessionModal ? this.renderSessionModal() : ""}
            ${this.showSettingsModal ? this.renderSettingsModal() : ""}
            ${this.showNotificationCenter ? this.renderNotificationCenter() : ""}
            ${this.masterPasswordDialog.visible ? this.renderMasterPasswordDialog() : ""}
            ${this.renderToasts()}
        `;

    this.bindEvents();
    this.positionFloatingMenus();
    this.attachActiveTerminal();
    this.scrollChatToBottom();
  }

  private positionFloatingMenus(): void {
    requestAnimationFrame(() => {
      const viewportPadding = 8;
      const viewportWidth = window.innerWidth;
      const viewportHeight = window.innerHeight;

      const clamp = (
        value: number,
        min: number,
        max: number,
      ): number => Math.min(Math.max(value, min), Math.max(min, max));

      const positionMenu = (
        menu: HTMLElement,
        preferredLeft: number,
        preferredTop: number,
      ): boolean => {
        const rect = menu.getBoundingClientRect();
        const left = clamp(
          preferredLeft,
          viewportPadding,
          viewportWidth - rect.width - viewportPadding,
        );
        let top = preferredTop;

        if (top + rect.height > viewportHeight - viewportPadding) {
          top = preferredTop - rect.height - 14;
        }
        top = clamp(
          top,
          viewportPadding,
          viewportHeight - rect.height - viewportPadding,
        );

        const changed =
          Math.abs(rect.left - left) > 0.5 || Math.abs(rect.top - top) > 0.5;
        if (changed) {
          menu.style.left = `${Math.round(left)}px`;
          menu.style.top = `${Math.round(top)}px`;
        }
        return changed;
      };

      const contextMenu =
        root?.querySelector<HTMLElement>("[data-session-context-menu]");
      if (contextMenu && this.sessionContextMenu.visible) {
        positionMenu(
          contextMenu,
          this.sessionContextMenu.x,
          this.sessionContextMenu.y,
        );
      }

      const sidebarMenu =
        root?.querySelector<HTMLElement>("[data-sidebar-actions-menu]");
      const sidebarTrigger =
        root?.querySelector<HTMLButtonElement>(
          "[data-toggle-sidebar-actions-menu]",
        );
      if (sidebarMenu && sidebarTrigger && this.showSidebarActionsMenu) {
        const triggerRect = sidebarTrigger.getBoundingClientRect();
        positionMenu(
          sidebarMenu,
          triggerRect.right - sidebarMenu.getBoundingClientRect().width,
          triggerRect.bottom + 7,
        );
      }
    });
  }

  private bindEvents(): void {
    root
      ?.querySelector<HTMLDivElement>("[data-session-context-overlay]")
      ?.addEventListener("click", () => {
        this.hideSessionContextMenu();
      });
    root
      ?.querySelectorAll<HTMLButtonElement>("[data-session-context-open]")
      .forEach((button) => {
        button.addEventListener("click", async () => {
          const profileID = button.dataset.sessionContextOpen;
          this.hideSessionContextMenu();
          if (profileID) {
            await this.openProfile(profileID);
          }
        });
      });
    root
      ?.querySelectorAll<HTMLButtonElement>("[data-session-context-delete]")
      .forEach((button) => {
        button.addEventListener("click", async () => {
          const profileID = button.dataset.sessionContextDelete;
          this.hideSessionContextMenu();
          if (profileID) {
            await this.runAction(
              async () => DeleteSessionProfile(profileID),
              "Unable to delete session profile",
            );
          }
        });
      });
    root
      ?.querySelectorAll<HTMLButtonElement>("[data-session-context-edit]")
      .forEach((button) => {
        button.addEventListener("click", () => {
          const profileID = button.dataset.sessionContextEdit;
          this.hideSessionContextMenu();
          if (profileID) {
            this.openSessionModalForEdit(profileID);
          }
        });
      });

    root
      ?.querySelector<HTMLButtonElement>("[data-open-session-modal]")
      ?.addEventListener("click", () => {
        this.closeSidebarActionsMenu();
        this.openSessionModalForCreate();
      });

    root
      ?.querySelectorAll<HTMLButtonElement>("[data-open-settings-tab]")
      .forEach((button) => {
        button.addEventListener("click", () => {
          this.closeSidebarActionsMenu();
          this.settingsTab =
            (button.dataset.openSettingsTab as SettingsTab) ?? "ai";
          if (this.settingsTab === "commandpolicy") {
            this.commandPolicyTab = "access";
          }
          this.showSettingsModal = true;
          this.initializeSettingsDrafts();
          this.render();
          if (this.settingsTab === "ai") {
            void this.loadCloudModels(false);
          }
        });
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-toggle-sidebar-actions-menu]")
      ?.addEventListener("click", () => {
        this.showSidebarActionsMenu = !this.showSidebarActionsMenu;
        this.render();
        if (this.showSidebarActionsMenu) {
          this.focusFirstSidebarActionsMenuItem();
        }
      });
    root
      ?.querySelector<HTMLDivElement>("[data-sidebar-actions-menu-overlay]")
      ?.addEventListener("click", () => {
        this.closeSidebarActionsMenu(true);
      });
    root
      ?.querySelector<HTMLDivElement>("[data-sidebar-actions-menu]")
      ?.addEventListener("keydown", (event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          this.closeSidebarActionsMenu(true);
          return;
        }
        if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
          return;
        }
        const items = Array.from(
          root?.querySelectorAll<HTMLButtonElement>(
            ".sidebar-actions-menu-item",
          ) ?? [],
        );
        if (items.length === 0) {
          return;
        }
        event.preventDefault();
        const activeIndex = items.indexOf(
          document.activeElement as HTMLButtonElement,
        );
        if (event.key === "Home") {
          items[0]?.focus();
          return;
        }
        if (event.key === "End") {
          items[items.length - 1]?.focus();
          return;
        }
        if (event.key === "ArrowDown") {
          const nextIndex =
            activeIndex >= 0 ? (activeIndex + 1) % items.length : 0;
          items[nextIndex]?.focus();
          return;
        }
        const prevIndex =
          activeIndex >= 0
            ? (activeIndex - 1 + items.length) % items.length
            : items.length - 1;
        items[prevIndex]?.focus();
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-open-notification-center]")
      ?.addEventListener("click", () => {
        this.closeSidebarActionsMenu();
        this.showNotificationCenter = true;
        this.render();
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-toggle-sidebar-panel]")
      ?.addEventListener("click", () => {
        this.closeSidebarActionsMenu();
        this.sidebarCollapsed = !this.sidebarCollapsed;
        localStorage.setItem(
          SIDEBAR_COLLAPSED_KEY,
          String(this.sidebarCollapsed),
        );
        this.render();
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-toggle-assistant-panel]")
      ?.addEventListener("click", () => {
        this.assistantCollapsed = !this.assistantCollapsed;
        localStorage.setItem(
          ASSISTANT_COLLAPSED_KEY,
          String(this.assistantCollapsed),
        );
        this.render();
      });

    root
      ?.querySelectorAll<HTMLButtonElement>("[data-session-inner-tab]")
      .forEach((button) => {
        button.addEventListener("click", async () => {
          const activeTab = this.activeTab();
          if (!activeTab) {
            return;
          }
          this.sessionInnerTab = this.normalizeSessionInnerTab(
            activeTab.protocolId,
            (button.dataset.sessionInnerTab as SessionInnerTab) ??
              this.defaultSessionInnerTab(activeTab.protocolId),
          );
          this.storeSessionInnerTab(activeTab.id, this.sessionInnerTab);
          this.render();
          if (this.sessionInnerTab === "sftp") {
            await this.ensureActiveSFTPLoaded();
          }
        });
      });

    root
      ?.querySelectorAll<HTMLButtonElement>("[data-session-modal-tab]")
      .forEach((button) => {
        button.addEventListener("click", () => {
          this.syncSessionFormFromDOM();
          this.sessionModalTab =
            (button.dataset.sessionModalTab as SessionModalTab) ?? "host";
          this.render();
        });
      });
    root
      ?.querySelector<HTMLFormElement>("[data-session-form]")
      ?.addEventListener("input", (event) => {
        this.syncSessionFormFromDOM(event.target);
      });
    root
      ?.querySelector<HTMLSelectElement>('select[name="protocolId"]')
      ?.addEventListener("change", (event) => {
        const select = event.currentTarget as HTMLSelectElement;
        this.syncSessionFormFromDOM();
        this.sessionForm.protocolId = select.value || "ssh";
        this.render();
      });
    root
      ?.querySelector<HTMLSelectElement>('select[name="authMethod"]')
      ?.addEventListener("change", (event) => {
        this.syncSessionFormFromDOM();
        this.sessionForm.authMethod =
          (event.currentTarget as HTMLSelectElement).value === "key"
            ? "key"
            : "password";
        this.render();
      });

    root
      ?.querySelectorAll<HTMLElement>("[data-session-item]")
      .forEach((item) => {
        item.addEventListener("dblclick", async () => {
          const profileID = item.dataset.sessionItem;
          if (!profileID) {
            return;
          }
          await this.openProfile(profileID);
        });
        item.addEventListener("contextmenu", (event) => {
          event.preventDefault();
          const profileID = item.dataset.sessionItem;
          if (!profileID) {
            return;
          }
          this.sessionContextMenu = {
            visible: true,
            x: event.clientX,
            y: event.clientY,
            profileId: profileID,
          };
          this.render();
        });
      });

    root
      ?.querySelector<HTMLTextAreaElement>("[data-ssh-config-import]")
      ?.addEventListener("input", (event) => {
        this.sshConfigDraft = (
          event.currentTarget as HTMLTextAreaElement
        ).value;
      });

    root
      ?.querySelector<HTMLButtonElement>("[data-import-ssh-config]")
      ?.addEventListener("click", async () => {
        try {
          await ImportSSHConfig(this.sshConfigDraft);
          this.sshConfigDraft = "";
          await this.refresh("");
        } catch (error) {
          this.setErrorMessage(
            formatError("Unable to import SSH config", error),
          );
          this.render();
        }
      });

    root
      ?.querySelectorAll<HTMLButtonElement>("[data-tab-id]")
      .forEach((button) => {
        button.addEventListener("click", async () => {
          const tabID = button.dataset.tabId;
          if (!tabID) {
            return;
          }
          this.activeTabId = tabID;
          const activeTab = this.activeTab();
          this.sessionInnerTab = activeTab
            ? this.restoreSessionInnerTab(activeTab)
            : "console";
          if (this.sftpState.tabId !== tabID) {
            this.sftpState = this.defaultSFTPState(tabID);
          }
          this.render();
          if (this.sessionInnerTab === "sftp") {
            await this.ensureActiveSFTPLoaded();
          }
        });
      });

    root
      ?.querySelectorAll<HTMLButtonElement>("[data-reconnect-tab]")
      .forEach((button) => {
        button.addEventListener("click", async (event) => {
          event.stopPropagation();
          const tabID = button.dataset.reconnectTab;
          if (!tabID) return;
          await this.reconnectTab(tabID);
        });
      });

    root
      ?.querySelectorAll<HTMLButtonElement>("[data-close-tab]")
      .forEach((button) => {
        button.addEventListener("click", async (event) => {
          event.stopPropagation();
          const tabID = button.dataset.closeTab;
          if (!tabID) {
            return;
          }
          await this.closeTab(tabID);
        });
      });

    root
      ?.querySelector<HTMLButtonElement>("[data-refresh-sftp]")
      ?.addEventListener("click", async () => {
        const activeTab = this.activeTab();
        if (!activeTab) {
          return;
        }
        await this.loadSFTP(activeTab.id, this.sftpState.path);
      });

    root
      ?.querySelector<HTMLButtonElement>("[data-sftp-up]")
      ?.addEventListener("click", async () => {
        const activeTab = this.activeTab();
        if (!activeTab) {
          return;
        }
        await this.loadSFTP(activeTab.id, parentPath(this.sftpState.path));
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-sftp-upload]")
      ?.addEventListener("click", async () => {
        const activeTab = this.activeTab();
        if (!activeTab) {
          return;
        }
        await this.uploadToSFTP(activeTab.id);
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-sftp-download]")
      ?.addEventListener("click", async () => {
        const activeTab = this.activeTab();
        if (!activeTab) {
          return;
        }
        await this.downloadFromSFTP(activeTab.id);
      });

    root
      ?.querySelectorAll<HTMLButtonElement>("[data-sftp-dir]")
      .forEach((button) => {
        button.addEventListener("click", async () => {
          const targetPath = button.dataset.sftpDir;
          const activeTab = this.activeTab();
          if (!targetPath || !activeTab) {
            return;
          }
          await this.loadSFTP(activeTab.id, targetPath);
        });
      });
    root
      ?.querySelectorAll<HTMLButtonElement>("[data-sftp-file-edit]")
      .forEach((button) => {
        button.addEventListener("click", async () => {
          const targetPath = button.dataset.sftpFileEdit;
          const activeTab = this.activeTab();
          if (!targetPath || !activeTab) {
            return;
          }
          await this.openRemoteFile(activeTab.id, targetPath);
        });
      });
    root
      ?.querySelectorAll<HTMLInputElement>("[data-sftp-select-file]")
      .forEach((input) => {
        input.addEventListener("change", () => {
          const targetPath = input.dataset.sftpSelectFile;
          if (!targetPath) {
            return;
          }
          this.toggleSFTPFileSelection(targetPath, input.checked);
        });
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-save-remote-file]")
      ?.addEventListener("click", async () => {
        const activeTab = this.activeTab();
        if (!activeTab || !this.sftpState.editorPath) {
          return;
        }
        await this.saveRemoteFile(activeTab.id);
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-close-remote-editor]")
      ?.addEventListener("click", () => {
        this.closeRemoteEditor();
      });
    root
      ?.querySelector<HTMLTextAreaElement>("[data-remote-editor]")
      ?.addEventListener("input", (event) => {
        this.sftpState.editorContent = (
          event.currentTarget as HTMLTextAreaElement
        ).value;
        this.sftpState.editorDirty = true;
      });

    root
      ?.querySelector<HTMLFormElement>("[data-cloud-form]")
      ?.addEventListener("submit", async (event) => {
        event.preventDefault();
        const model = this.cloudDraftModel;
        const endpoint = this.cloudDraftEndpoint;
        const token = this.cloudDraftToken;
        await this.runAction(
          async () => SaveCloudProvider(model, endpoint, token),
          "Unable to save cloud provider",
        );
      });
    root
      ?.querySelectorAll<HTMLButtonElement>("[data-select-ai-provider]")
      .forEach((button) => {
        button.addEventListener("click", async () => {
          const providerID = button.dataset.selectAiProvider;
          if (!providerID) {
            return;
          }
          await this.runAction(
            async () => SelectAIProvider(providerID),
            "Unable to switch AI provider",
          );
        });
      });
    root
      ?.querySelector<HTMLInputElement>("[data-cloud-model]")
      ?.addEventListener("input", (event) => {
        this.cloudDraftModel = (event.currentTarget as HTMLInputElement).value;
      });
    root
      ?.querySelector<HTMLInputElement>("[data-cloud-endpoint]")
      ?.addEventListener("input", (event) => {
        this.cloudDraftEndpoint = (
          event.currentTarget as HTMLInputElement
        ).value;
      });
    root
      ?.querySelector<HTMLInputElement>("[data-cloud-token]")
      ?.addEventListener("input", (event) => {
        this.cloudDraftToken = (event.currentTarget as HTMLInputElement).value;
      });
    root
      ?.querySelector<HTMLInputElement>("[data-local-model-url]")
      ?.addEventListener("input", (event) => {
        this.localDraftDownloadURL = (
          event.currentTarget as HTMLInputElement
        ).value;
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-load-cloud-models]")
      ?.addEventListener("click", async () => {
        await this.loadCloudModels(true);
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-start-cloud-auth]")
      ?.addEventListener("click", async () => {
        await this.startCloudProviderAuth();
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-save-local-provider]")
      ?.addEventListener("click", async () => {
        await this.runAction(
          async () => SaveLocalProvider(this.currentLocalDownloadURL()),
          "Unable to save local model settings",
        );
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-download-local-model]")
      ?.addEventListener("click", async () => {
        await this.runAction(
          async () => DownloadLocalModel(this.currentLocalDownloadURL()),
          "Unable to download local model",
        );
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-start-local-model]")
      ?.addEventListener("click", async () => {
        await this.runAction(
          async () => StartLocalModel(),
          "Unable to start local model",
        );
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-stop-local-model]")
      ?.addEventListener("click", async () => {
        await this.runAction(
          async () => StopLocalModel(),
          "Unable to stop local model",
        );
      });

    const chatForm = root?.querySelector<HTMLFormElement>("[data-chat-form]");
    chatForm?.addEventListener("submit", async (event) => {
      event.preventDefault();
      this.syncChatFormFromDOM();
      const message = this.chatDraftMessage;
      if (!message.trim()) return;
      const payload = message;
      this.aiStatus = "thinking";
      this.render();
      try {
        const sent = await this.withMasterPasswordRetry(
          () => SendPlainChatMessage(payload),
          "Master password setup was cancelled, so the saved AI provider token remains locked.",
        );
        if (typeof sent === "undefined") {
          this.aiStatus = "idle";
          this.render();
          return;
        }
        this.chatDraftMessage = "";
        await this.refresh("");
      } catch (error) {
        this.aiStatus = "idle";
        this.setErrorMessage(formatError("Unable to send message", error));
        this.render();
      }
    });
    chatForm?.addEventListener("input", () => {
      this.syncChatFormFromDOM();
    });
    chatForm?.addEventListener("change", () => {
      this.syncChatFormFromDOM();
    });
    chatForm
      ?.querySelector<HTMLTextAreaElement>('textarea[name="message"]')
      ?.addEventListener("keydown", (event) => {
        if (event.ctrlKey && event.key === "Enter") {
          event.preventDefault();
          const form = root?.querySelector<HTMLFormElement>("[data-chat-form]");
          form?.requestSubmit();
        }
      });

    root
      ?.querySelector<HTMLButtonElement>("[data-clear-chat]")
      ?.addEventListener("click", async () => {
        try {
          await ClearChat();
          this.aiStatus = "idle";
          await this.refresh("");
        } catch (error) {
          this.setErrorMessage(formatError("Unable to clear chat", error));
          this.render();
        }
      });

    root
      ?.querySelectorAll<HTMLButtonElement>("[data-command-policy-tab]")
      .forEach((button) => {
        button.addEventListener("click", () => {
          this.commandPolicyTab =
            (button.dataset.commandPolicyTab as CommandPolicyTab) ?? "access";
          this.render();
        });
      });
    root
      ?.querySelectorAll<HTMLInputElement>("[data-command-tool-enabled]")
      .forEach((input) => {
        input.addEventListener("change", async () => {
          const toolID = input.dataset.commandToolEnabled;
          if (!toolID) return;
          await this.persistCommandPolicy((policy) => {
            policy.tools = policy.tools.map((tool) =>
              tool.id === toolID ? { ...tool, enabled: input.checked } : tool,
            );
          });
        });
      });
    root
      ?.querySelectorAll<HTMLButtonElement>("[data-command-request-action]")
      .forEach((button) => {
        button.addEventListener("click", async () => {
          const requestID = button.dataset.commandRequestId;
          const mode = button.dataset.commandRequestAction;
          if (!requestID || !mode) return;
          await this.runAction(
            async () => ResolveCommandPolicyRequest(requestID, mode),
            "Unable to resolve command request",
          );
          await this.refresh("");
        });
      });
    root
      ?.querySelector<HTMLFormElement>("[data-command-docs-form]")
      ?.addEventListener("submit", async (event) => {
        event.preventDefault();
        const path =
          (
            event.currentTarget as HTMLFormElement
          ).querySelector<HTMLInputElement>('input[name="localDocsPath"]')
            ?.value ?? "";
        await this.persistCommandPolicy((policy) => {
          policy.localDocsPath = path.trim();
        });
      });

    root
      ?.querySelectorAll<HTMLElement>("[data-close-modal]")
      .forEach((button) => {
        button.addEventListener("click", () => {
          const wasSessionModalOpen = this.showSessionModal;
          this.showSessionModal = false;
          this.showSettingsModal = false;
          this.showNotificationCenter = false;
          if (wasSessionModalOpen) {
            this.editingProfileID = "";
            this.sessionForm = this.defaultSessionForm();
            this.sessionTagDraft = "";
            this.sessionTagInputVisible = false;
            this.sessionNameAuto = true;
            this.sessionModalTab = "host";
          }
          this.render();
        });
      });

    root
      ?.querySelector<HTMLFormElement>("[data-master-password-form]")
      ?.addEventListener("submit", async (event) => {
        event.preventDefault();
        const password = this.masterPasswordDialog.password;
        if (!password) {
          this.masterPasswordDialog.error = "Enter a master password.";
          this.render();
          return;
        }
        if (
          this.masterPasswordDialog.mode === "create" &&
          password !== this.masterPasswordDialog.confirm
        ) {
          this.masterPasswordDialog.error =
            "Master password confirmation does not match.";
          this.render();
          return;
        }
        try {
          await EnsureMasterPassword(password);
          this.resolveMasterPasswordDialog(true);
          await this.refresh("");
        } catch (error) {
          this.masterPasswordDialog.error = formatError(
            this.masterPasswordDialog.mode === "create"
              ? "Unable to create master password"
              : "Unable to unlock secure storage",
            error,
          );
          this.masterPasswordDialog.password = "";
          this.masterPasswordDialog.confirm = "";
          this.render();
        }
      });
    root
      ?.querySelector<HTMLInputElement>("[data-master-password-input]")
      ?.addEventListener("input", (event) => {
        this.masterPasswordDialog.password = (
          event.currentTarget as HTMLInputElement
        ).value;
        this.masterPasswordDialog.error = "";
      });
    root
      ?.querySelector<HTMLInputElement>("[data-master-password-confirm]")
      ?.addEventListener("input", (event) => {
        this.masterPasswordDialog.confirm = (
          event.currentTarget as HTMLInputElement
        ).value;
        this.masterPasswordDialog.error = "";
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-master-password-cancel]")
      ?.addEventListener("click", () => {
        this.resolveMasterPasswordDialog(false);
      });

    root
      ?.querySelectorAll<HTMLButtonElement>("[data-set-theme]")
      .forEach((button) => {
        button.addEventListener("click", () => {
          const t = button.dataset.setTheme as Theme;
          if (isTheme(t)) {
            this.theme = t;
            this.applyTheme();
            this.render();
          }
        });
      });

    root
      ?.querySelectorAll<HTMLButtonElement>("[data-set-terminal-theme]")
      .forEach((button) => {
        button.addEventListener("click", () => {
          const t = button.dataset.setTerminalTheme as TerminalTheme;
          if (isTerminalTheme(t)) this.setTerminalTheme(t);
        });
      });

    root
      ?.querySelector<HTMLFormElement>("[data-vault-settings-form]")
      ?.addEventListener("submit", async (event) => {
        event.preventDefault();
        if (!this.shellState) return;
        const updated = {
          ...this.shellState.settings,
          vaultAddress: this.vaultDraftAddress,
          vaultMountPoint: this.vaultDraftMountPoint,
          vaultAuthMethod: this.vaultDraftAuthMethod,
          vaultLogin: this.vaultDraftLogin,
          vaultToken: this.vaultDraftToken,
          vaultPassword: this.vaultDraftPassword,
          vaultAutoRenewToken: this.vaultDraftAutoRenewToken,
        } as unknown as settingsModels.AppSettings;
        await this.runAction(
          async () => UpdateSettings(updated),
          "Unable to save Vault settings",
        );
      });
    root
      ?.querySelector<HTMLFormElement>("[data-keepass-settings-form]")
      ?.addEventListener("submit", async (event) => {
        event.preventDefault();
        if (!this.shellState) return;
        const updated = {
          ...this.shellState.settings,
          keepassDatabasePath: this.vaultDraftKeePassDatabasePath,
          keepassPassword: this.vaultDraftKeePassPassword,
        } as unknown as settingsModels.AppSettings;
        await this.runAction(
          async () => UpdateSettings(updated),
          "Unable to save KeePass settings",
        );
      });
    root
      ?.querySelector<HTMLInputElement>("[data-vault-address]")
      ?.addEventListener("input", (event) => {
        this.vaultDraftAddress = (
          event.currentTarget as HTMLInputElement
        ).value;
      });
    root
      ?.querySelector<HTMLInputElement>("[data-vault-mount]")
      ?.addEventListener("input", (event) => {
        this.vaultDraftMountPoint = (
          event.currentTarget as HTMLInputElement
        ).value;
      });
    root
      ?.querySelector<HTMLSelectElement>("[data-vault-auth-method]")
      ?.addEventListener("change", (event) => {
        const value = (event.currentTarget as HTMLSelectElement).value;
        this.vaultDraftAuthMethod = normalizeVaultAuthMethod(value);
        this.render();
      });
    root
      ?.querySelector<HTMLInputElement>("[data-vault-login]")
      ?.addEventListener("input", (event) => {
        this.vaultDraftLogin = (event.currentTarget as HTMLInputElement).value;
      });
    root
      ?.querySelector<HTMLInputElement>("[data-vault-token]")
      ?.addEventListener("input", (event) => {
        this.vaultDraftToken = (event.currentTarget as HTMLInputElement).value;
      });
    root
      ?.querySelector<HTMLInputElement>("[data-vault-password]")
      ?.addEventListener("input", (event) => {
        this.vaultDraftPassword = (
          event.currentTarget as HTMLInputElement
        ).value;
      });
    root
      ?.querySelector<HTMLInputElement>("[data-vault-renew]")
      ?.addEventListener("change", (event) => {
        this.vaultDraftAutoRenewToken = (
          event.currentTarget as HTMLInputElement
        ).checked;
      });
    root
      ?.querySelector<HTMLInputElement>("[data-keepass-db-path]")
      ?.addEventListener("input", (event) => {
        this.vaultDraftKeePassDatabasePath = (
          event.currentTarget as HTMLInputElement
        ).value;
      });
    root
      ?.querySelector<HTMLInputElement>("[data-keepass-password]")
      ?.addEventListener("input", (event) => {
        this.vaultDraftKeePassPassword = (
          event.currentTarget as HTMLInputElement
        ).value;
      });
    root
      ?.querySelectorAll<HTMLButtonElement>("[data-toggle-session-tag-filter]")
      .forEach((button) => {
        button.addEventListener("click", () => {
          const tag = String(
            button.dataset.toggleSessionTagFilter ?? "",
          ).trim();
          if (!tag) {
            return;
          }
          this.toggleSessionTagFilter(tag);
        });
      });

    root
      ?.querySelector<HTMLFormElement>("[data-pf-add-form]")
      ?.addEventListener("submit", async (event) => {
        event.preventDefault();
        if (!this.shellState) return;
        const form = event.currentTarget as HTMLFormElement;
        const localPort = (
          form.querySelector<HTMLInputElement>('input[name="pfLocalPort"]')
            ?.value ?? ""
        ).trim();
        const remoteHost = (
          form.querySelector<HTMLInputElement>('input[name="pfRemoteHost"]')
            ?.value ?? ""
        ).trim();
        const remotePort = (
          form.querySelector<HTMLInputElement>('input[name="pfRemotePort"]')
            ?.value ?? ""
        ).trim();
        const hostId = (
          form.querySelector<HTMLSelectElement>('select[name="pfHostId"]')
            ?.value ?? ""
        ).trim();
        if (!localPort || !hostId || !remoteHost || !remotePort) return;
        const rules: PortForwardRule[] = [
          ...(this.shellState.settings.portForwardRules ?? []),
          { localPort, remoteHost, remotePort, hostId, enabled: true },
        ];
        const updated = {
          ...this.shellState.settings,
          portForwardRules: rules,
        } as unknown as settingsModels.AppSettings;
        this.pfNewLocalPort = "";
        this.pfNewRemoteHost = "";
        this.pfNewRemotePort = "";
        this.pfNewHostId = "";
        await this.runAction(
          async () => UpdateSettings(updated),
          "Unable to save port forwarding rule",
        );
      });
    root
      ?.querySelectorAll<HTMLButtonElement>("[data-pf-start-stop]")
      .forEach((button) => {
        button.addEventListener("click", async () => {
          if (!this.shellState) return;
          const idx = Number(button.dataset.pfStartStop);
          const rules: PortForwardRule[] = (
            this.shellState.settings.portForwardRules ?? []
          ).map((r, i) => (i === idx ? { ...r, enabled: !r.enabled } : r));
          const updated = {
            ...this.shellState.settings,
            portForwardRules: rules,
          } as unknown as settingsModels.AppSettings;
          await this.runAction(
            async () => UpdateSettings(updated),
            "Unable to update port forwarding rule state",
          );
        });
      });
    root
      ?.querySelectorAll<HTMLButtonElement>("[data-pf-delete]")
      .forEach((button) => {
        button.addEventListener("click", async () => {
          if (!this.shellState) return;
          const idx = Number(button.dataset.pfDelete);
          const rules: PortForwardRule[] = (
            this.shellState.settings.portForwardRules ?? []
          ).filter((_, i) => i !== idx);
          const updated = {
            ...this.shellState.settings,
            portForwardRules: rules,
          } as unknown as settingsModels.AppSettings;
          await this.runAction(
            async () => UpdateSettings(updated),
            "Unable to delete port forwarding rule",
          );
        });
      });
    root
      ?.querySelector<HTMLInputElement>('input[name="pfLocalPort"]')
      ?.addEventListener("input", (event) => {
        this.pfNewLocalPort = (event.currentTarget as HTMLInputElement).value;
      });
    root
      ?.querySelector<HTMLInputElement>('input[name="pfRemoteHost"]')
      ?.addEventListener("input", (event) => {
        this.pfNewRemoteHost = (event.currentTarget as HTMLInputElement).value;
      });
    root
      ?.querySelector<HTMLInputElement>('input[name="pfRemotePort"]')
      ?.addEventListener("input", (event) => {
        this.pfNewRemotePort = (event.currentTarget as HTMLInputElement).value;
      });
    root
      ?.querySelector<HTMLSelectElement>('select[name="pfHostId"]')
      ?.addEventListener("change", (event) => {
        this.pfNewHostId = (event.currentTarget as HTMLSelectElement).value;
      });

    root
      ?.querySelector<HTMLFormElement>("[data-session-form]")
      ?.addEventListener("submit", async (event) => {
        event.preventDefault();
        const form = event.currentTarget as HTMLFormElement;
        const formData = new FormData(form);
        this.commitSessionTagDraft();
        const protocolId = String(formData.get("protocolId") ?? "ssh");
        const authMethod =
          String(formData.get("authMethod") ?? "password") === "key"
            ? "key"
            : "password";
        const privateKeyPath = String(formData.get("privateKeyPath") ?? "");
        const host = String(formData.get("host") ?? "").trim();
        const name = String(formData.get("name") ?? "").trim() || host;
        const profile: SessionProfile = {
          id: this.editingProfileID,
          name,
          group: "",
          host,
          port: Number(
            formData.get("port") ?? 22,
          ),
          username: String(formData.get("username") ?? ""),
          password: String(
            authMethod === "password" ? (formData.get("password") ?? "") : "",
          ),
          keyPassphrase: String(
            authMethod === "key" ? (formData.get("keyPassphrase") ?? "") : "",
          ),
          protocolId,
          tags: this.sessionForm.tags,
          favorite: false,
        };
        const proxyJump = String(formData.get("proxyJump") ?? "").trim();
        const localForwards = String(
          formData.get("localForwards") ?? "",
        ).trim();
        const useSSHAgent = formData.get("useSSHAgent") === "on";
        const keyPath = privateKeyPath.trim();
        if (authMethod === "key" && !keyPath) {
          this.setErrorMessage(
            "Unable to save session profile: private key path is required for key auth",
          );
          this.render();
          return;
        }
        const options: Record<string, string> = {};
        options.auth_method = authMethod;
        if (authMethod === "key" && keyPath) {
          options.ssh_private_key_path = keyPath;
        }
        if (this.supportsSSHAdvancedOptions(protocolId) && proxyJump) {
          options.proxy_jump = proxyJump;
        }
        if (this.supportsSSHAdvancedOptions(protocolId) && localForwards) {
          options.local_forwards = localForwards;
        }
        if (this.supportsSSHAdvancedOptions(protocolId) && useSSHAgent) {
          options.use_ssh_agent = "true";
        }
        if (Object.keys(options).length > 0) {
          profile.options = options;
        }
        try {
          const saved = await this.withMasterPasswordRetry(
            () => CreateSessionProfile(profile),
            "Master password setup was cancelled, so the session profile was not saved.",
          );
          if (typeof saved === "undefined") {
            return;
          }
          this.showSessionModal = false;
          this.editingProfileID = "";
          this.sessionForm = this.defaultSessionForm();
          this.sessionTagDraft = "";
          this.sessionTagInputVisible = false;
          this.sessionNameAuto = true;
          this.sessionModalTab = "host";
          await this.refresh("");
        } catch (error) {
          this.setErrorMessage(
            formatError("Unable to save session profile", error),
          );
          this.render();
        }
      });

    root
      ?.querySelector<HTMLButtonElement>("[data-accept-host-key]")
      ?.addEventListener("click", async () => {
        const { tabId, profileId } = this.hostKeyDialog;
        this.hostKeyDialog = {
          visible: false,
          tabId: "",
          profileId: "",
          fingerprint: "",
          hostname: "",
        };
        try {
          await AcceptSSHHostKey(tabId);
          await this.retrySSHConnect(tabId, profileId);
        } catch (error) {
          this.setErrorMessage(formatError("Unable to accept host key", error));
          this.render();
        }
      });

    root
      ?.querySelector<HTMLButtonElement>("[data-reject-host-key]")
      ?.addEventListener("click", () => {
        this.hostKeyDialog = {
          visible: false,
          tabId: "",
          profileId: "",
          fingerprint: "",
          hostname: "",
        };
        this.render();
      });

    root
      ?.querySelector<HTMLButtonElement>("[data-session-tag-add-open]")
      ?.addEventListener("click", () => {
        this.sessionTagInputVisible = true;
        this.render();
        requestAnimationFrame(() => {
          root
            ?.querySelector<HTMLInputElement>("[data-session-tag-input]")
            ?.focus();
        });
      });

    root
      ?.querySelector<HTMLInputElement>("[data-session-tag-input]")
      ?.addEventListener("input", (event) => {
        this.sessionTagDraft = (event.currentTarget as HTMLInputElement).value;
      });

    root
      ?.querySelector<HTMLInputElement>("[data-session-tag-input]")
      ?.addEventListener("keydown", (event) => {
        if (event.key !== "Enter") {
          return;
        }
        event.preventDefault();
        this.commitSessionTagDraft();
        this.render();
      });

    root
      ?.querySelector<HTMLInputElement>("[data-session-tag-input]")
      ?.addEventListener("blur", () => {
        this.commitSessionTagDraft();
        requestAnimationFrame(() => this.render());
      });
    root
      ?.querySelector<HTMLButtonElement>("[data-clear-notifications]")
      ?.addEventListener("click", () => {
        this.notifications = [];
        this.toastQueue = [];
        this.render();
      });
    root
      ?.querySelectorAll<HTMLButtonElement>("[data-delete-notification]")
      .forEach((button) => {
        button.addEventListener("click", () => {
          const id = String(button.dataset.deleteNotification ?? "");
          if (!id) {
            return;
          }
          this.notifications = this.notifications.filter(
            (notification) => notification.id !== id,
          );
          this.toastQueue = this.toastQueue.filter(
            (notification) => notification.id !== id,
          );
          this.render();
        });
      });

    root
      ?.querySelectorAll<HTMLButtonElement>("[data-session-tag-remove]")
      .forEach((button) => {
        button.addEventListener("click", () => {
          const tag = String(button.dataset.sessionTagRemove ?? "").trim();
          if (!tag) {
            return;
          }
          this.sessionForm.tags = this.sessionForm.tags.filter(
            (entry) => entry !== tag,
          );
          this.render();
        });
      });
  }

  private async openProfile(profileID: string): Promise<void> {
    const profile = this.shellState?.sessionProfiles.find(
      (entry) => entry.id === profileID,
    );
    if (!profile) {
      return;
    }
    try {
      const tab = await LaunchSession(profileID);
      this.activeTabId = tab.id;
      this.sessionInnerTab = this.defaultSessionInnerTab(profile.protocolId);
      this.storeSessionInnerTab(tab.id, this.sessionInnerTab);
      await this.refresh("");
      if (profile.protocolId === "ssh") {
        this.ensureTerminalSubscription(tab.id);
        if (await this.connectSSHWithHostKeyHandling(tab.id, profileID)) {
          await this.refresh("");
          this.fitActiveTerminal();
          await this.ensureActiveSFTPLoaded(true);
        }
      } else {
        await ReconnectSession(tab.id);
        await this.refresh("");
      }

    } catch (error) {
      this.setErrorMessage(formatError("Unable to open session", error));
      this.render();
    }
  }

  private async connectSSHWithHostKeyHandling(
    tabId: string,
    profileId: string,
  ): Promise<boolean> {
    try {
      const connected = await this.withMasterPasswordRetry(
        () => ConnectSSH(tabId, profileId),
        "Master password setup was cancelled, so the saved SSH credentials remain locked.",
      );
      if (typeof connected === "undefined") {
        return false;
      }
      return true;
    } catch (error) {
      const msg = error instanceof Error ? error.message : String(error);
      if (msg.includes("unknown host key")) {
        const fpMatch = msg.match(/fingerprint\s+(\S+)/);
        const hostMatch = msg.match(/unknown host key:\s*(\S+)/);
        this.hostKeyDialog = {
          visible: true,
          tabId,
          profileId,
          fingerprint: fpMatch ? fpMatch[1] : "",
          hostname: hostMatch ? hostMatch[1] : "",
        };
        this.render();
        return false;
      }
      throw error;
    }
  }

  private async reconnectTab(tabID: string): Promise<void> {
    const tab = this.shellState?.activeSessions.find((entry) => entry.id === tabID);
    if (!tab) return;
    try {
      this.activeTabId = tabID;
      if (tab.protocolId === "ssh") {
        this.ensureTerminalSubscription(tabID);
        if (!(await this.connectSSHWithHostKeyHandling(tabID, tab.profileId))) return;
      } else {
        await ReconnectSession(tabID);
      }
      await this.refresh("");
      this.fitActiveTerminal();
      if (tab.protocolId === "ssh") await this.ensureActiveSFTPLoaded(true);
    } catch (error) {
      this.setErrorMessage(formatError("Unable to reconnect session", error));
      this.render();
    }
  }

  private async retrySSHConnect(
    tabId: string,
    profileId: string,
  ): Promise<void> {
    try {
      this.ensureTerminalSubscription(tabId);
      const connected = await this.withMasterPasswordRetry(
        () => ConnectSSH(tabId, profileId),
        "Master password setup was cancelled, so the saved SSH credentials remain locked.",
      );
      if (typeof connected === "undefined") {
        return;
      }
      this.fitActiveTerminal();
      await this.refresh("");
      await this.ensureActiveSFTPLoaded(true);
    } catch (error) {
      this.setErrorMessage(formatError("Unable to connect SSH", error));
      this.render();
    }
  }

  private async closeTab(tabID: string): Promise<void> {
    const terminal = this.terminals.get(tabID);
    terminal?.unsubscribe?.();
    terminal?.terminal.dispose();
    terminal?.wrapper.remove();
    this.terminals.delete(tabID);
    try {
      await DisconnectSSH(tabID);
    } catch {
      // ignored - backend CloseSession will clean up too.
    }
    await this.runAction(
      async () => CloseSession(tabID),
      "Unable to close session",
    );
    this.sessionInnerTabs.delete(tabID);
    this.persistSessionInnerTabs();
    await this.ensureActiveSFTPLoaded(true);
  }

  private async loadSFTP(tabID: string, targetPath: string): Promise<void> {
    this.sftpState = {
      ...this.sftpState,
      tabId: tabID,
      loading: true,
      error: "",
    };
    this.render();
    try {
      const entries = targetPath
        ? await NavigateSFTP(tabID, targetPath)
        : await ListSFTPFiles(tabID, "");
      this.sftpState = {
        ...this.sftpState,
        tabId: tabID,
        path: inferDirectory(entries, targetPath),
        entries,
        loading: false,
        error: "",
        selectedFiles: [],
      };
    } catch (error) {
      this.pushNotification(
        "error",
        formatError("Unable to load SFTP files", error),
      );
      this.sftpState = {
        ...this.sftpState,
        tabId: tabID,
        loading: false,
        error: formatError("Unable to load SFTP files", error),
      };
    }
    this.render();
  }

  private async openRemoteFile(
    tabID: string,
    targetPath: string,
  ): Promise<void> {
    this.sftpState = {
      ...this.sftpState,
      tabId: tabID,
      editorOpen: true,
      editorPath: targetPath,
      editorLoading: true,
      editorError: "",
    };
    this.render();
    try {
      const content = await ReadSFTPFile(tabID, targetPath);
      this.sftpState = {
        ...this.sftpState,
        editorOpen: true,
        editorPath: targetPath,
        editorContent: content,
        editorLoading: false,
        editorDirty: false,
        editorError: "",
      };
    } catch (error) {
      this.pushNotification(
        "error",
        formatError("Unable to read remote file", error),
      );
      this.sftpState = {
        ...this.sftpState,
        editorOpen: true,
        editorLoading: false,
        editorError: formatError("Unable to read remote file", error),
      };
    }
    this.render();
  }

  private async saveRemoteFile(tabID: string): Promise<void> {
    this.sftpState = { ...this.sftpState, editorSaving: true, editorError: "" };
    this.render();
    try {
      await SaveSFTPFile(
        tabID,
        this.sftpState.editorPath,
        this.sftpState.editorContent,
      );
      this.sftpState = {
        ...this.sftpState,
        editorSaving: false,
        editorDirty: false,
        editorError: "",
      };
    } catch (error) {
      this.pushNotification(
        "error",
        formatError("Unable to save remote file", error),
      );
      this.sftpState = {
        ...this.sftpState,
        editorSaving: false,
        editorError: formatError("Unable to save remote file", error),
      };
    }
    this.render();
  }

  private async uploadToSFTP(tabID: string): Promise<void> {
    this.sftpState = { ...this.sftpState, error: "" };
    this.render();
    try {
      const localPaths = await SelectUploadFiles();
      if (!localPaths || localPaths.length === 0) {
        return;
      }
      await UploadSFTPFiles(tabID, this.sftpState.path || ".", localPaths);
      await this.loadSFTP(tabID, this.sftpState.path || ".");
    } catch (error) {
      this.pushNotification(
        "error",
        formatError("Unable to upload files", error),
      );
      this.sftpState = {
        ...this.sftpState,
        error: formatError("Unable to upload files", error),
      };
      this.render();
    }
  }

  private async downloadFromSFTP(tabID: string): Promise<void> {
    if (this.sftpState.selectedFiles.length === 0) {
      this.sftpState = {
        ...this.sftpState,
        error: "Select at least one file to download.",
      };
      this.render();
      return;
    }
    try {
      const localDir = await SelectDownloadDirectory();
      if (!localDir) {
        return;
      }
      await DownloadSFTPFiles(tabID, localDir, this.sftpState.selectedFiles);
      this.sftpState = { ...this.sftpState, selectedFiles: [], error: "" };
      this.render();
    } catch (error) {
      this.pushNotification(
        "error",
        formatError("Unable to download files", error),
      );
      this.sftpState = {
        ...this.sftpState,
        error: formatError("Unable to download files", error),
      };
      this.render();
    }
  }

  private toggleSFTPFileSelection(targetPath: string, checked: boolean): void {
    const selected = new Set(this.sftpState.selectedFiles);
    if (checked) {
      selected.add(targetPath);
    } else {
      selected.delete(targetPath);
    }
    this.sftpState = { ...this.sftpState, selectedFiles: Array.from(selected) };
    this.render();
  }

  private attachActiveTerminal(): void {
    const host = document.querySelector<HTMLDivElement>("#terminal-host");
    if (!host) {
      return;
    }
    host.innerHTML = "";

    const activeTab = this.activeTab();
    if (!activeTab) {
      host.innerHTML =
        '<div class="empty-state">Open an SSH session to start a terminal.</div>';
      return;
    }
    if (activeTab.protocolId !== "ssh") {
      host.innerHTML = `<div class="empty-state">${escapeHtml(activeTab.protocolId.toUpperCase())} session opened in tab mode. Terminal is available for SSH tabs.</div>`;
      return;
    }

    const terminalState = this.ensureTerminalSubscription(activeTab.id);
    host.appendChild(terminalState.wrapper);
    if (!terminalState.opened) {
      terminalState.terminal.open(terminalState.inner);
      terminalState.fitAddon.fit();
      terminalState.opened = true;
      terminalState.terminal.focus();
    }
    this.fitActiveTerminal();
  }

  private ensureTerminalSubscription(tabID: string): TerminalState {
    const existing = this.terminals.get(tabID);
    if (existing) {
      return existing;
    }

    const wrapper = document.createElement("div");
    wrapper.className = "terminal-pane";
    const inner = document.createElement("div");
    inner.className = "terminal-instance";
    wrapper.appendChild(inner);

    const terminal = new Terminal({
      cursorBlink: true,
      fontFamily:
        "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
      theme: this.terminalThemeOptions(),
      scrollback: 2000,
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.loadAddon(new WebLinksAddon());
    terminal.writeln("Connecting...");
    terminal.onData((data: string) => {
      void SendSSHInput(tabID, data).catch((error) => {
        this.setErrorMessage(
          formatError("Unable to send terminal input", error),
        );
        this.render();
      });
    });

    const unsubscribe = EventsOn(
      `terminal:output:${tabID}`,
      (...payload: unknown[]) => {
        const data = payload[0] as { data?: string } | undefined;
        const chunk = data?.data ?? "";
        terminal.write(chunk);
        if (!chunk) {
          return;
        }
      },
    );

    const terminalState: TerminalState = {
      terminal,
      fitAddon,
      wrapper,
      inner,
      opened: false,
      unsubscribe,
    };
    this.terminals.set(tabID, terminalState);
    return terminalState;
  }

  private fitActiveTerminal(): void {
    const activeTab = this.activeTab();
    if (!activeTab) {
      return;
    }
    const terminalState = this.terminals.get(activeTab.id);
    if (!terminalState || !terminalState.opened) {
      return;
    }
    requestAnimationFrame(() => {
      terminalState.fitAddon.fit();
      const dimensions =
        terminalState.terminal.cols > 0 && terminalState.terminal.rows > 0;
      if (dimensions) {
        void ResizeTerminal(
          activeTab.id,
          terminalState.terminal.cols,
          terminalState.terminal.rows,
        ).catch(() => undefined);
      }
    });
  }

  private scrollChatToBottom(): void {
    const messages = document.querySelector<HTMLDivElement>("#chat-messages");
    if (messages) {
      messages.scrollTop = messages.scrollHeight;
    }
  }

  private syncChatFormFromDOM(): void {
    const form = root?.querySelector<HTMLFormElement>("[data-chat-form]");
    if (!form) {
      return;
    }
    this.chatDraftMessage =
      form.querySelector<HTMLTextAreaElement>('textarea[name="message"]')
        ?.value ?? "";
  }

  private renderSidebarPanel(): string {
    if (this.sidebarCollapsed) {
      return `
                <aside class="panel sidebar-panel collapsed">
                    <div class="panel-header panel-header-collapsed">
                        <button class="icon-button panel-toggle-button collapsed-panel-toggle" data-toggle-sidebar-panel title="Expand sessions panel" aria-label="Expand sessions panel">
                            <span>▶</span>
                            <span class="collapsed-panel-label">Sessions</span>
                        </button>
                    </div>
                </aside>
            `;
    }

    return `
            <aside class="panel sidebar-panel">
                <div class="panel-header">
                    <div>
                        <div class="eyebrow">Workspace</div>
                        <h1>Eiksy</h1>
                    </div>
                    <div class="panel-header-actions">
                        <div class="sidebar-actions-menu-wrap">
                            <button class="icon-button" data-toggle-sidebar-actions-menu title="Session manager menu" aria-label="Session manager menu" aria-haspopup="true" aria-expanded="${this.showSidebarActionsMenu ? "true" : "false"}" aria-controls="${this.sidebarActionsMenuID}">☰</button>
                            ${
                              this.showSidebarActionsMenu
                                ? `
                                <div class="sidebar-actions-menu-overlay" data-sidebar-actions-menu-overlay></div>
                                <div class="sidebar-actions-menu" id="${this.sidebarActionsMenuID}" data-sidebar-actions-menu role="menu">
                                    <button class="sidebar-actions-menu-item" data-open-session-modal role="menuitem"><span class="sidebar-actions-menu-icon">+</span><span>New session</span></button>
                                    <button class="sidebar-actions-menu-item" data-open-settings-tab="ai" role="menuitem"><span class="sidebar-actions-menu-icon">🤖</span><span>AI settings</span></button>
                                    <button class="sidebar-actions-menu-item" data-open-settings-tab="commandpolicy" role="menuitem"><span class="sidebar-actions-menu-icon">🛡️</span><span>Command Policy</span></button>
                                    <button class="sidebar-actions-menu-item" data-open-settings-tab="sshconfig" role="menuitem"><span class="sidebar-actions-menu-icon">📥</span><span>SSH Config import</span></button>
                                    <button class="sidebar-actions-menu-item" data-open-settings-tab="portforward" role="menuitem"><span class="sidebar-actions-menu-icon">🔀</span><span>Port forwarding</span></button>
                                    <button class="sidebar-actions-menu-item" data-open-settings-tab="theme" role="menuitem"><span class="sidebar-actions-menu-icon">🎨</span><span>Theme</span></button>
                                    <button class="sidebar-actions-menu-item" data-open-settings-tab="about" role="menuitem"><span class="sidebar-actions-menu-icon">ℹ️</span><span>About</span></button>
                                </div>
                            `
                                : ""
                            }
                        </div>
                        <button class="icon-button notification-button" data-open-notification-center title="Notifications" aria-label="Notifications${this.notifications.length > 0 ? `, ${this.notifications.length} unread` : ""}">${this.notifications.length > 0 ? `<span class="notification-count digits-${Math.min(String(this.notifications.length > 99 ? 99 : this.notifications.length).length + (this.notifications.length > 99 ? 1 : 0), 4)}">${this.notifications.length > 99 ? "99+" : this.notifications.length}</span>` : "🔔"}</button>
                        <button class="icon-button panel-toggle-button" data-toggle-sidebar-panel title="Collapse sessions panel" aria-label="Collapse sessions panel">◀</button>
                    </div>
                </div>
                <div class="sidebar-body">
                    <section class="section sidebar-section sessions-section">
                        <div class="section-heading">
                            <span class="section-title">Sessions</span>
                        </div>
                        <div class="section-copy">Double-click a session to open it. Right-click to manage.</div>
                        <div class="session-list">
                            ${this.renderSessionProfiles()}
                        </div>
                    </section>
                    <section class="section sidebar-section session-tags-filter-section">
                        <div class="section-heading">
                            <span class="section-title">Tags</span>
                        </div>
                        ${this.renderSessionTagFilters()}
                    </section>
                </div>
            </aside>
        `;
  }

  private renderAssistantPanel(): string {
    if (this.assistantCollapsed) {
      return `
                <aside class="panel assistant-panel collapsed">
                    <div class="panel-header panel-header-collapsed">
                        <button class="icon-button panel-toggle-button collapsed-panel-toggle" data-toggle-assistant-panel title="Expand assistant panel" aria-label="Expand assistant panel">
                            <span>◀</span>
                            <span class="collapsed-panel-label">Assistant</span>
                        </button>
                    </div>
                </aside>
            `;
    }

    return `
            <aside class="panel assistant-panel">
                <div class="panel-header">
                    <div>
                        <div class="eyebrow">Assistant</div>
                        <h2>AI</h2>
                    </div>
                    <div class="panel-header-actions">
                        ${this.hasConfiguredProvider() ? `<button class="icon-button" data-clear-chat title="Clear chat">🗑</button>` : ""}
                        <button class="icon-button panel-toggle-button" data-toggle-assistant-panel title="Collapse assistant panel" aria-label="Collapse assistant panel">▶</button>
                    </div>
                </div>
                <section class="section chat-section">
                    <div class="section-title">Assistant</div>
                    <div class="chat-messages" id="chat-messages">
                        ${this.renderMessages()}
                    </div>
                </section>
                ${
                  this.hasConfiguredProvider()
                    ? `
                <form class="chat-input-form" data-chat-form>
                    ${this.aiStatus === "thinking" ? '<div class="ai-status-indicator">⏳ Thinking…</div>' : ""}
                    <textarea class="chat-textarea" name="message" placeholder="Ask the assistant… (Ctrl+Enter to send)" aria-label="Assistant message" rows="3">${escapeHtml(this.chatDraftMessage)}</textarea>
                    <button class="action-button" type="submit" ${this.aiStatus === "thinking" ? "disabled" : ""}>Send</button>
                </form>
                `
                    : ""
                }
            </aside>
        `;
  }

  private renderSessionProfiles(): string {
    if (!this.shellState || this.shellState.sessionProfiles.length === 0) {
      return '<div class="empty-state">No saved sessions yet.</div>';
    }
    const filteredProfiles = this.filteredSessionProfiles();
    if (filteredProfiles.length === 0) {
      return '<div class="empty-state">No sessions for selected tags.</div>';
    }
    return filteredProfiles
      .map(
        (profile) => `
            <article class="session-card" data-session-item="${escapeHtml(profile.id)}">
                <div>
                    <div class="session-title-row">
                        <strong>${escapeHtml(profile.name)}</strong>
                        <span class="pill small">${escapeHtml(profile.protocolId.toUpperCase())}</span>
                    </div>
                    <div class="session-meta">${escapeHtml(profile.username)}@${escapeHtml(profile.host)}:${escapeHtml(String(profile.port))}</div>
                    <div class="session-tags">${profile.tags.map((tag) => `<span>${escapeHtml(tag)}</span>`).join("")}</div>
                </div>
            </article>
        `,
      )
      .join("");
  }

  private renderSessionTagFilters(): string {
    const tags = this.allSessionTags();
    const hasUntagged = (this.shellState?.sessionProfiles ?? []).some(
      (profile) => (profile.tags ?? []).length === 0,
    );
    const filterKeys = hasUntagged ? [...tags, this.untaggedFilterTag] : tags;
    if (filterKeys.length === 0) {
      return '<div class="section-copy">No tags yet.</div>';
    }
    return `
            <div class="tag-filter-list">
                ${filterKeys
                  .map(
                    (tag) => `
                    <button
                        class="tag-filter-item ${this.selectedSessionTags.has(tag) ? "active" : ""}"
                        data-toggle-session-tag-filter="${escapeHtml(tag)}"
                        type="button"
                    >${tag === this.untaggedFilterTag ? "Untagged" : escapeHtml(tag)}</button>
                `,
                  )
                  .join("")}
            </div>
        `;
  }

  private renderTabs(): string {
    if (!this.shellState || this.shellState.activeSessions.length === 0) {
      return '<div class="tab empty">No active sessions</div>';
    }
    return this.shellState.activeSessions
      .map(
        (tab) => `
            <button class="tab ${tab.id === this.activeTabId ? "active" : ""}" data-tab-id="${escapeHtml(tab.id)}">
                <span>${escapeHtml(tab.title)}</span>
                <span class="pill small">${escapeHtml(tab.status || "unknown")}</span>
                ${tab.status !== "connected" ? '<span class="tab-action" data-reconnect-tab="${escapeHtml(tab.id)}" title="Reconnect">↻</span>' : ""}
                <span class="tab-close" data-close-tab="${escapeHtml(tab.id)}">×</span>
            </button>
        `,
      )
      .join("");
  }

  private renderSFTPActions(): string {
    const activeTab = this.activeTab();
    if (!activeTab || activeTab.protocolId !== "ssh") {
      return '<span class="section-copy">Select an SSH tab</span>';
    }
    const showRefresh =
      !this.sftpState.loading &&
      (this.sftpState.entries.length > 0 || !!this.sftpState.error);
    const canTransfer =
      !this.sftpState.loading &&
      !this.sftpState.error &&
      this.sftpState.entries.length > 0;
    return `
            <div class="sftp-actions">
                ${showRefresh ? '<button class="icon-button sftp-action-button" data-refresh-sftp title="Reload current path" aria-label="Reload current path">↻</button>' : ""}
                ${canTransfer ? '<button class="icon-button sftp-action-button" data-sftp-upload title="Upload files" aria-label="Upload files">⤴</button>' : ""}
                ${canTransfer ? `<button class="icon-button sftp-action-button" data-sftp-download ${this.sftpState.selectedFiles.length === 0 ? "disabled" : ""} title="Download selected files" aria-label="Download selected files">⤵${this.sftpState.selectedFiles.length > 0 ? ` ${this.sftpState.selectedFiles.length}` : ""}</button>` : ""}
            </div>
        `;
  }

  private renderSFTPBrowser(): string {
    const activeTab = this.activeTab();
    if (!activeTab) {
      return '<div class="empty-state">Open a session tab to browse files.</div>';
    }
    if (activeTab.protocolId !== "ssh") {
      return '<div class="empty-state">SFTP browsing is available for SSH tabs.</div>';
    }
    if (this.sftpState.loading) {
      return '<div class="empty-state">Loading files…</div>';
    }
    if (this.sftpState.error) {
      return `<div class="error-banner compact">${escapeHtml(this.sftpState.error)}</div>`;
    }
    if (this.sftpState.entries.length === 0) {
      return '<div class="empty-state">SFTP opens automatically for the active SSH session.</div>';
    }
    return `
            <div class="sftp-explorer">
                <div class="sftp-path-row">
                    <span class="sftp-path">${escapeHtml(this.sftpState.path || ".")}</span>
                    <button class="icon-button sftp-action-button" data-sftp-up title="Go to parent directory" aria-label="Go to parent directory">↑</button>
                </div>
                <div class="sftp-list">
                    <div class="sftp-list-header">
                        <span></span>
                        <span>Name</span>
                        <span>Size</span>
                        <span>Mode</span>
                        <span>Modified</span>
                        <span></span>
                    </div>
                    ${this.sftpState.entries
                      .map((entry) =>
                        entry.isDir
                          ? `<button class="sftp-row sftp-row-button sftp-dir" data-sftp-dir="${escapeHtml(entry.path)}">
                            <span></span>
                            <span class="sftp-row-name"><span class="sftp-entry-icon" aria-hidden="true">📁</span><span>${escapeHtml(entry.name)}</span></span>
                            <span>—</span>
                            <span>${escapeHtml(entry.mode || "—")}</span>
                            <span>${escapeHtml(entry.modTime || "—")}</span>
                            <span></span>
                        </button>`
                          : `<div class="sftp-row sftp-file ${this.sftpState.selectedFiles.includes(entry.path) ? "selected" : ""}">
                            <span class="sftp-row-check"><input type="checkbox" data-sftp-select-file="${escapeHtml(entry.path)}" ${this.sftpState.selectedFiles.includes(entry.path) ? "checked" : ""} /></span>
                            <span class="sftp-row-name"><span class="sftp-entry-icon" aria-hidden="true">📄</span><span>${escapeHtml(entry.name)}</span></span>
                            <span>${escapeHtml(formatBytes(entry.size))}</span>
                            <span>${escapeHtml(entry.mode || "—")}</span>
                            <span>${escapeHtml(entry.modTime || "—")}</span>
                            <span><button class="action-button secondary sftp-inline-button" data-sftp-file-edit="${escapeHtml(entry.path)}">Edit</button></span>
                        </div>`,
                      )
                      .join("")}
                </div>
            </div>
        `;
  }

  private renderRemoteEditorModal(): string {
    if (!this.sftpState.editorOpen) {
      return "";
    }
    return `
            <div class="remote-editor">
                <div class="modal-overlay">
                    <div class="modal-dialog wide remote-editor-dialog">
                        <div class="panel-header compact-header">
                            <div>
                                <div class="eyebrow">SFTP editor</div>
                                <h2>${escapeHtml(this.sftpState.editorPath || "Remote file")}</h2>
                            </div>
                            <div class="section-actions">
                                <button class="action-button secondary" data-close-remote-editor>Close</button>
                                <button class="action-button secondary" data-save-remote-file ${this.sftpState.editorSaving || this.sftpState.editorLoading ? "disabled" : ""}>${this.sftpState.editorSaving ? "Saving…" : "Save"}</button>
                            </div>
                        </div>
                        <div class="modal-body remote-editor-body">
                            ${
                              this.sftpState.editorLoading
                                ? '<div class="empty-state">Loading file…</div>'
                                : `
                                ${this.sftpState.editorError ? `<div class="error-banner compact">${escapeHtml(this.sftpState.editorError)}</div>` : ""}
                                ${
                                  this.sftpState.editorError &&
                                  !this.sftpState.editorContent
                                    ? ""
                                    : `<textarea class="remote-editor-input" data-remote-editor>${escapeHtml(this.sftpState.editorContent)}</textarea>`
                                }
                            `
                            }
                        </div>
                    </div>
                </div>
            </div>
        `;
  }

  private async loadCloudModels(force: boolean): Promise<void> {
    const provider = this.cloudProvider();
    if (!provider) {
      return;
    }
    const endpoint = (
      this.cloudDraftEndpoint ||
      provider.endpoint ||
      ""
    ).trim();
    const token = (this.cloudDraftToken || "").trim();
    if (!endpoint) {
      this.cloudModels = [];
      this.cloudModelsEndpoint = "";
      this.cloudModelsError = "Enter endpoint first.";
      this.render();
      return;
    }
    if (
      !force &&
      endpoint === this.cloudModelsEndpoint &&
      this.cloudModels.length > 0
    ) {
      return;
    }
    this.cloudModelsLoading = true;
    this.cloudModelsError = "";
    this.render();
    try {
      const models = await this.withMasterPasswordRetry(
        () => ListCloudModels(endpoint, token),
        "Master password setup was cancelled, so the saved AI provider token remains locked.",
      );
      if (!models) {
        this.cloudModels = [];
        this.cloudModelsLoading = false;
        this.render();
        return;
      }
      this.cloudModels = models;
      this.cloudModelsEndpoint = endpoint;
      this.cloudModelsError =
        models.length > 0 ? "" : "No models returned by API.";
    } catch (error) {
      this.cloudModels = [];
      this.cloudModelsEndpoint = endpoint;
      this.cloudModelsError = formatError("Unable to load models", error);
      this.pushNotification("error", this.cloudModelsError);
    } finally {
      this.cloudModelsLoading = false;
      this.render();
    }
  }

  private async startCloudProviderAuth(): Promise<void> {
    const provider = this.cloudProvider();
    const endpoint = (
      this.cloudDraftEndpoint ||
      provider?.endpoint ||
      ""
    ).trim();
    if (!endpoint) {
      this.setErrorMessage("Enter endpoint first.");
      return;
    }

    this.clearCloudAuthPolling();
    this.cloudAuthPending = true;
    this.cloudAuthMessage = "Opening browser authorization…";
    this.render();

    try {
      const session = await StartCloudProviderAuth(endpoint);
      this.cloudAuthSessionId = session.id;
      this.cloudAuthMessage =
        session.message || "Waiting for browser authorization.";
      if (session.authUrl) {
        BrowserOpenURL(session.authUrl);
      }
      this.scheduleCloudAuthPoll(session.id);
      this.render();
    } catch (error) {
      this.cloudAuthPending = false;
      this.cloudAuthMessage = "";
      this.setErrorMessage(
        formatError("Unable to start browser authorization", error),
      );
      this.render();
    }
  }

  private scheduleCloudAuthPoll(sessionID: string): void {
    this.clearCloudAuthPolling();
    this.cloudAuthPollTimer = window.setTimeout(() => {
      void this.pollCloudProviderAuthSession(sessionID);
    }, 1000);
  }

  private clearCloudAuthPolling(): void {
    if (this.cloudAuthPollTimer !== null) {
      window.clearTimeout(this.cloudAuthPollTimer);
      this.cloudAuthPollTimer = null;
    }
  }

  private async pollCloudProviderAuthSession(sessionID: string): Promise<void> {
    try {
      const session = await GetCloudProviderAuthSession(sessionID);
      this.applyCloudProviderAuthSession(session);
    } catch (error) {
      if (sessionID !== this.cloudAuthSessionId) {
        return;
      }
      this.cloudAuthPending = false;
      this.cloudAuthMessage = "";
      this.setErrorMessage(
        formatError("Unable to finish browser authorization", error),
      );
      this.render();
    }
  }

  private applyCloudProviderAuthSession(
    session: CloudProviderAuthSession,
  ): void {
    if (session.id !== this.cloudAuthSessionId) {
      return;
    }

    this.cloudAuthMessage = session.message || "";
    if (session.status === "completed") {
      this.cloudAuthPending = false;
      this.clearCloudAuthPolling();
      this.cloudAuthMessage =
        session.message || "Token received from browser authorization.";
      this.pushNotification("info", this.cloudAuthMessage);
      this.render();
      return;
    }

    if (session.status === "failed" || session.status === "expired") {
      this.cloudAuthPending = false;
      this.clearCloudAuthPolling();
      const message =
        session.message || "Browser authorization did not complete.";
      this.cloudAuthMessage = message;
      this.pushNotification(
        session.status === "expired" ? "warn" : "error",
        message,
      );
      this.render();
      return;
    }

    this.cloudAuthPending = true;
    this.scheduleCloudAuthPoll(session.id);
    this.render();
  }

  private supportsCloudProviderBrowserAuth(endpoint: string): boolean {
    const normalized = endpoint.trim();
    if (!normalized) {
      return false;
    }
    try {
      const url = new URL(normalized);
      return /\/\.?api\/llm\/openai(?:\/|$)/.test(url.pathname);
    } catch {
      return false;
    }
  }

  private renderProviderSetup(): string {
    const provider = this.selectedProvider();
    if (!provider) {
      return '<div class="empty-state">No AI provider configured yet.</div>';
    }
    return `
            <div class="provider-form ai-provider-mode-card">
                <div class="section-title">Model source</div>
                <div class="provider-form-actions ai-provider-mode-actions">
                    ${(this.shellState?.ai.providers ?? [])
                      .map(
                        (entry) => `
                        <button
                            class="action-button ${entry.selected ? "" : "secondary"}"
                            type="button"
                            data-select-ai-provider="${escapeHtml(entry.id)}"
                        >${escapeHtml(entry.name)}</button>
                    `,
                      )
                      .join("")}
                </div>
                <div class="section-copy">Choose either a local model or a cloud endpoint.</div>
            </div>
            ${provider.class === "local_openai" ? this.renderLocalProviderSetup(provider) : this.renderCloudProviderSetup(provider)}
        `;
  }

  private renderCloudProviderSetup(provider: AIProvider): string {
    const selectedModel = this.cloudDraftModel || provider.model || "";
    const browserAuthSupported = this.supportsCloudProviderBrowserAuth(
      this.cloudDraftEndpoint || provider.endpoint || "",
    );
    return `
            <form class="provider-form" data-cloud-form>
                <div class="section-title">Cloud model</div>
                <label>
                    <span>Model</span>
                    <input name="model" data-cloud-model type="text" value="${escapeHtml(selectedModel)}" list="cloud-model-suggestions" placeholder="gpt-5.6" required />
                    <datalist id="cloud-model-suggestions">
                        ${this.cloudModels.map((model) => `<option value="${escapeHtml(model)}"></option>`).join("")}
                    </datalist>
                </label>
                <label>
                    <span>Endpoint</span>
                    <input type="url" data-cloud-endpoint name="endpoint" value="${escapeHtml(this.cloudDraftEndpoint || provider.endpoint || "")}" placeholder="https://api.example.com/v1" required />
                </label>
                <label>
                    <span>API token (optional)</span>
                    <input type="password" data-cloud-token name="token" value="${escapeHtml(this.cloudDraftToken)}" placeholder="sk-..." />
                </label>
                ${provider.hasToken && !this.cloudDraftToken ? '<div class="section-copy">A token is already saved in encrypted storage. Leave the field blank to keep it.</div>' : ""}
                ${
                  browserAuthSupported
                    ? `
                    <div class="provider-form-actions">
                        <button class="action-button secondary" type="button" data-start-cloud-auth ${this.cloudAuthPending ? "disabled" : ""}>${this.cloudAuthPending ? "Waiting for browser auth…" : "Get token via browser auth"}</button>
                        <span class="section-copy">For Sourcegraph/Cody-compatible endpoints.</span>
                    </div>
                    ${this.cloudAuthMessage ? `<div class="section-copy">${escapeHtml(this.cloudAuthMessage)}</div>` : ""}
                `
                    : '<div class="section-copy">Browser auth is available for Sourcegraph/Cody-compatible endpoints.</div>'
                }
                <div class="provider-form-actions">
                    <button class="action-button secondary" type="button" data-load-cloud-models ${this.cloudModelsLoading ? "disabled" : ""}>${this.cloudModelsLoading ? "Loading models…" : "Load models"}</button>
                    ${this.cloudModelsError ? `<span class="section-copy">${escapeHtml(this.cloudModelsError)}</span>` : ""}
                </div>
                <div class="provider-form-actions">
                    <button class="action-button" type="submit">Save cloud provider</button>
                </div>
            </form>
        `;
  }

  private renderLocalProviderSetup(provider: AIProvider): string {
    const downloadURL =
      this.localDraftDownloadURL || provider.downloadUrl || "";
    return `
            <div class="provider-form">
                <div class="section-title">Local Qwen3 4B</div>
                <label>
                    <span>Model build</span>
                    <input type="text" value="Qwen3 4B · Q4_K_M" readonly />
                </label>
                <label>
                    <span>Model URL</span>
                    <input type="url" data-local-model-url value="${escapeHtml(downloadURL)}" placeholder="https://…" required />
                </label>
                <div class="section-copy">You can keep the default Qwen3 GGUF link or provide your own model URL.</div>
                <div class="provider-form-actions">
                    <button class="action-button secondary" type="button" data-save-local-provider>Save link</button>
                    <button class="action-button secondary" type="button" data-download-local-model>Download model</button>
                </div>
                <div class="provider-form-actions">
                    <button class="action-button ${provider.status === "running" ? "secondary" : ""}" type="button" data-start-local-model ${provider.status === "running" ? "disabled" : ""}>${provider.status === "running" ? "Local model is running" : "Start local model"}</button>
                    <button class="action-button secondary" type="button" data-stop-local-model ${provider.status === "running" ? "" : "disabled"}>Stop model</button>
                </div>
                <div class="section-copy">Status: ${escapeHtml(provider.status || "unknown")}</div>
                ${provider.localPath ? `<div class="section-copy">File: ${escapeHtml(provider.localPath)}</div>` : ""}
                <div class="section-copy">Endpoint: ${escapeHtml(provider.endpoint || "http://127.0.0.1:8012/v1")}</div>
                <div class="section-copy">Requires <code>llama-server</code> in PATH for local launch.</div>
            </div>
        `;
  }

  private renderPortForwardRules(): string {
    const rules: PortForwardRule[] =
      this.shellState?.settings.portForwardRules ?? [];
    const sshProfiles = (this.shellState?.sessionProfiles ?? []).filter(
      (p) => p.protocolId === "ssh",
    );
    const profileName = (hostId: string) => {
      const p = sshProfiles.find((s) => s.id === hostId);
      return p ? `${p.name} (${p.host})` : hostId;
    };
    if (rules.length === 0) {
      return '<div class="empty-state" style="padding:0.75rem 0;">No rules yet. Add one below.</div>';
    }
    return `
            <div class="pf-rules-list">
                ${rules
                  .map(
                    (rule, idx) => `
                    <div class="pf-rule ${rule.enabled ? "pf-rule-enabled" : "pf-rule-disabled"}">
                        <span class="pf-rule-ports">${escapeHtml(rule.localPort || "")}</span>
                        <span class="pf-rule-arrow">→</span>
                        <span class="pf-rule-host">${escapeHtml(rule.remoteHost)}:${escapeHtml(rule.remotePort)} via ${escapeHtml(profileName(rule.hostId))}</span>
                        <span class="pf-rule-spacer"></span>
                        <button class="action-button secondary" data-pf-start-stop="${idx}" title="${rule.enabled ? "Stop" : "Start"}">${rule.enabled ? "Stop" : "Start"}</button>
                        <button class="icon-button danger" data-pf-delete="${idx}" title="Delete rule">✕</button>
                    </div>
                `,
                  )
                  .join("")}
            </div>
        `;
  }

  private renderMessages(): string {
    if (!this.shellState) {
      return "";
    }
    if (!this.hasConfiguredProvider()) {
      return '<div class="empty-state">Configure a provider in Settings to start chatting.</div>';
    }
    const messages = (this.shellState.ai.messages ?? [])
      .map(
        (message) => `
            <div class="message ${escapeClassName(message.role)}">${message.role === "assistant" ? renderMarkdown(message.content) : escapeHtml(message.content)}</div>
        `,
      )
      .join("");
    return this.renderAIActivities() + messages;
  }

  private renderAIActivities(): string {
    return this.aiActivities
      .map((activity) => {
        const status = escapeHtml(activity.status.replaceAll("_", " "));
        const approval = activity.approval ? ` · ${escapeHtml(activity.approval)}` : "";
        const details = [
          activity.exitCode >= 0 ? `exit ${activity.exitCode}` : "",
          activity.durationMs > 0 ? `${activity.durationMs} ms` : "",
          activity.errorType ? activity.errorType : "",
        ].filter(Boolean).join(" · ");
        const output = activity.stdout || activity.stderr
          ? `<details class="ai-operation-output"><summary>Output</summary>${activity.stdout ? `<pre>${escapeHtml(activity.stdout)}</pre>` : ""}${activity.stderr ? `<pre>${escapeHtml(activity.stderr)}</pre>` : ""}</details>`
          : "";
        return `
          <div class="ai-operation ai-operation-${escapeClassName(activity.status)}">
            <div class="ai-operation-header"><strong>Operate · ${status}</strong><span>${approval}</span></div>
            <div class="ai-operation-command"><code>${escapeHtml(activity.command)}</code></div>
            <div class="ai-operation-meta">Session: ${escapeHtml(activity.sessionId || "unknown")}${details ? ` · ${escapeHtml(details)}` : ""}</div>
            ${activity.error ? `<div class="ai-operation-error">${escapeHtml(activity.error)}</div>` : ""}
            ${activity.message ? `<div class="ai-operation-message">${escapeHtml(activity.message)}</div>` : ""}
            ${output}
          </div>
        `;
      })
      .join("");
  }
  private renderCommandPolicyPanel(): string {
    const policy = this.commandPolicy();
    const pendingRows =
      policy.pendingRequests.length === 0
        ? '<div class="empty-state" style="padding:0.75rem 0;">No pending permission requests.</div>'
        : policy.pendingRequests
            .map(
              (request) => `
                <div class="command-policy-row">
                    <div>
                        <div><strong>${escapeHtml(request.toolId)}</strong> → <code>${escapeHtml(request.command)}</code></div>
                        <div class="section-copy">Session: ${escapeHtml(request.sessionId || "not specified")}${request.reason ? ` · Reason: ${escapeHtml(request.reason)}` : ""}</div>
                    </div>
                    <div class="provider-form-actions">
                        <button class="action-button secondary" type="button" data-command-request-action="now" data-command-request-id="${escapeHtml(request.id)}">Run now</button>
                        <button class="action-button secondary" type="button" data-command-request-action="session" data-command-request-id="${escapeHtml(request.id)}">Allow for session</button>
                        <button class="action-button secondary" type="button" data-command-request-action="always" data-command-request-id="${escapeHtml(request.id)}">Always allow</button>
                        <button class="action-button secondary" type="button" data-command-request-action="deny" data-command-request-id="${escapeHtml(request.id)}">Deny</button>
                    </div>
                </div>
            `,
            )
            .join("");
    const toolRows = policy.tools
      .map(
        (tool) => `
            <div class="command-policy-row">
                <div>
                    <strong>${escapeHtml(tool.name || tool.id)}</strong>
                    <div class="section-copy">${escapeHtml(tool.description || "")}</div>
                </div>
                <div class="provider-form-actions">
                    <label class="inline-check"><span>Enabled</span><input type="checkbox" data-command-tool-enabled="${escapeHtml(tool.id)}" ${tool.enabled ? "checked" : ""} /></label>
                </div>
            </div>
        `,
      )
      .join("");
    const panelByTab: Record<CommandPolicyTab, string> = {
      access: `
                <div class="section-title">Access control</div>
                <div class="section-copy">Pending model requests are resolved explicitly and become command rules.</div>
                ${pendingRows}
            `,
      tools: `
                <div class="section-title">Tools</div>
                <div class="section-copy">Enable or disable tools available to the model.</div>
                <div>${toolRows || '<div class="empty-state" style="padding:0.75rem 0;">No tools configured.</div>'}</div>
            `,
      settings: `
                <div class="section-title">Tool settings</div>
                <form class="provider-form" data-command-docs-form>
                    <label>
                        <span>Local documentation path</span>
                        <input name="localDocsPath" value="${escapeHtml(policy.localDocsPath || "")}" placeholder="/path/to/docs" />
                    </label>
                    <div class="section-copy">This path is provided to the model to guide tool usage.</div>
                    <div class="provider-form-actions">
                        <button class="action-button" type="submit">Save path</button>
                    </div>
                </form>
            `,
    };
    return `
            <nav class="modal-tabs">
                <button class="modal-tab ${this.commandPolicyTab === "access" ? "active" : ""}" data-command-policy-tab="access">Access</button>
                <button class="modal-tab ${this.commandPolicyTab === "tools" ? "active" : ""}" data-command-policy-tab="tools">Tools</button>
                <button class="modal-tab ${this.commandPolicyTab === "settings" ? "active" : ""}" data-command-policy-tab="settings">Settings</button>
            </nav>
            <div class="modal-tab-panel active">
                ${panelByTab[this.commandPolicyTab]}
            </div>
        `;
  }

  private renderSettingsModal(): string {
    const titleByTab: Record<SettingsTab, string> = {
      ai: "AI settings",
      commandpolicy: "Command Policy",
      sshconfig: "SSH Config import",
      portforward: "Port forwarding",
      theme: "Theme",
      about: "About",
    };
    const currentYear = new Date().getFullYear();
    const sshProfiles = (this.shellState?.sessionProfiles ?? []).filter(
      (profile) => profile.protocolId === "ssh",
    );
    const settingsContentByTab: Record<SettingsTab, string> = {
      ai: `
                <div class="section-title">AI Provider</div>
                ${this.renderProviderSetup()}
            `,
      commandpolicy: this.renderCommandPolicyPanel(),
      sshconfig: `
                <div class="section-title">Import SSH Config</div>
                <div class="import-box">
                    <label class="import-label" for="ssh-config-import">Paste SSH config block</label>
                    <textarea id="ssh-config-import" data-ssh-config-import placeholder="Host prod&#10;  HostName prod.internal&#10;  User ops&#10;  ProxyJump bastion">${escapeHtml(this.sshConfigDraft)}</textarea>
                    <button class="action-button secondary" data-import-ssh-config>Import</button>
                </div>
            `,
      portforward: `
                <div class="section-title">Port forwarding rules</div>
                ${this.renderPortForwardRules()}
                <form class="provider-form" data-pf-add-form style="margin-top:0.5rem;">
                    <div style="display:grid;grid-template-columns:1fr 1fr 160px 1fr auto;gap:0.5rem;align-items:end;">
                        <label style="margin:0;"><span style="font-size:0.78rem;">Local port(s)</span><input type="text" name="pfLocalPort" value="${escapeHtml(this.pfNewLocalPort)}" placeholder="8080,9000-9005" /></label>
                        <label style="margin:0;"><span style="font-size:0.78rem;">Remote host</span><input type="text" name="pfRemoteHost" value="${escapeHtml(this.pfNewRemoteHost)}" placeholder="db.internal" /></label>
                        <label style="margin:0;"><span style="font-size:0.78rem;">Remote port</span><input type="number" name="pfRemotePort" min="1" max="65535" value="${escapeHtml(this.pfNewRemotePort)}" placeholder="5432" /></label>
                        <label style="margin:0;"><span style="font-size:0.78rem;">Target SSH host</span>
                            <select name="pfHostId">
                                <option value="">Select host</option>
                                ${sshProfiles.map((profile) => `<option value="${escapeHtml(profile.id)}" ${this.pfNewHostId === profile.id ? "selected" : ""}>${escapeHtml(profile.name)} (${escapeHtml(profile.host)})</option>`).join("")}
                            </select>
                        </label>
                        <button class="action-button" type="submit" style="align-self:flex-end;">Add rule</button>
                    </div>
                </form>
            `,
      theme: `
                <div class="section-title">Appearance</div>
                <div class="theme-settings-group">
                    <div class="theme-setting">
                        <span class="theme-setting-label">Application theme</span>
                        <div class="theme-switch">
                            <button class="theme-button theme-button-dark ${this.theme === "dark" ? "active" : ""}" data-set-theme="dark">Dark</button>
                            <button class="theme-button theme-button-light ${this.theme === "light" ? "active" : ""}" data-set-theme="light">Light</button>
                            <button class="theme-button theme-button-green ${this.theme === "green" ? "active" : ""}" data-set-theme="green">Green</button>
                        </div>
                    </div>
                    <div class="theme-setting">
                        <span class="theme-setting-label">SSH terminal theme</span>
                        <div class="theme-switch">
                            <button class="theme-button terminal-theme-button terminal-theme-black-white ${this.terminalTheme === "black-on-white" ? "active" : ""}" data-set-terminal-theme="black-on-white">Black on white</button>
                            <button class="theme-button terminal-theme-button terminal-theme-gray-black ${this.terminalTheme === "gray-on-black" ? "active" : ""}" data-set-terminal-theme="gray-on-black">Gray on black</button>
                            <button class="theme-button terminal-theme-button terminal-theme-green-black ${this.terminalTheme === "green-on-black" ? "active" : ""}" data-set-terminal-theme="green-on-black">Green on black</button>
                        </div>
                    </div>
                </div>
            `,
      about: `
                <div class="section-title">About</div>
                <div class="about-panel">
                    <img class="about-logo" src="${appLogo}" alt="Eiksy logo" />
                    <p class="about-copy">${APP_METADATA.name}</p>
                    <p class="about-copy">Version: ${this.appVersion}</p>
                    <p class="about-copy">© ${currentYear} ${APP_METADATA.copyright}.</p>
                    <p class="about-copy">License: ${APP_METADATA.license}</p>
                    <p class="about-copy">${APP_METADATA.legalNotice}</p>
                    <p class="about-copy about-links">
                        <a href="${APP_METADATA.repositoryUrl}" target="_blank" rel="noopener noreferrer">GitHub</a>
                        <span>•</span>
                        <a href="${APP_METADATA.latestReleaseUrl}" target="_blank" rel="noopener noreferrer">Latest release</a>
                    </p>
                </div>
            `,
    };
    return `
            <div class="modal-overlay">
                <div class="modal-dialog wide settings-dialog ${this.settingsTab === "about" ? "about-light" : ""}">
                    <div class="panel-header compact-header">
                        <div>
                            <div class="eyebrow">Settings window</div>
                            <h2>${titleByTab[this.settingsTab]}</h2>
                        </div>
                        <button class="icon-button" data-close-modal>×</button>
                    </div>
                    <div class="modal-body">
                        <div class="modal-tab-panel active">
                            ${settingsContentByTab[this.settingsTab]}
                        </div>
                    </div>
                </div>
            </div>
        `;
  }

  private openSessionModalForCreate(): void {
    this.editingProfileID = "";
    this.sessionForm = this.defaultSessionForm();
    this.sessionTagDraft = "";
    this.sessionTagInputVisible = false;
    this.sessionNameAuto = true;
    this.sessionModalTab = "host";
    this.showSessionModal = true;
    this.render();
  }

  private openSessionModalForEdit(profileID: string): void {
    const profile = this.shellState?.sessionProfiles.find(
      (entry) => entry.id === profileID,
    );
    if (!profile) {
      return;
    }
    this.editingProfileID = profileID;
    this.sessionForm = this.sessionFormFromProfile(profile);
    this.sessionTagDraft = "";
    this.sessionTagInputVisible = false;
    this.sessionNameAuto = false;
    this.sessionModalTab = "host";
    this.showSessionModal = true;
    this.render();
  }

  private sessionFormFromProfile(profile: sessions.Profile): SessionFormState {
    const options = profile.options ?? {};
    const authMethod =
      options.auth_method === "key" && this.supportsKeyAuth(profile.protocolId)
        ? "key"
        : "password";
    return {
      name: profile.name || profile.host || "",
      host: profile.host || "",
      port: String(profile.port || 22),
      username: profile.username || "",
      password: "",
      keyPassphrase: "",
      authMethod,
      privateKeyPath: options.ssh_private_key_path ?? "",
      protocolId: profile.protocolId || "ssh",
      tags: Array.isArray(profile.tags) ? [...profile.tags] : [],
      proxyJump: options.proxy_jump ?? "",
      localForwards: options.local_forwards ?? "",
      useSSHAgent: options.use_ssh_agent === "true",
      hasSavedPassword: Boolean(profile.hasPassword),
      hasSavedKeyPassphrase: Boolean(profile.hasKeyPassphrase),
    };
  }

  private renderSessionModal(): string {
    const tabs: Array<{ id: SessionModalTab; label: string }> = [
      { id: "host", label: "Host" },
      { id: "auth", label: "Authorization" },
      { id: "network", label: "Network" },
      { id: "other", label: "Other" },
    ];
    const supportsKeyAuth = this.supportsKeyAuth(this.sessionForm.protocolId);
    const supportsSSHAdvancedOptions = this.supportsSSHAdvancedOptions(
      this.sessionForm.protocolId,
    );
    const isEditing = this.editingProfileID !== "";
    return `
            <div class="modal-overlay">
                <div class="modal-dialog">
                    <div class="panel-header compact-header">
                        <div>
                            <div class="eyebrow">${isEditing ? "Session settings" : "New session"}</div>
                            <h2>${isEditing ? "Edit session profile" : "Create session profile"}</h2>
                        </div>
                        <button class="icon-button" data-close-modal>×</button>
                    </div>
                    <nav class="modal-tabs">
                        ${tabs.map((t) => `<button class="modal-tab ${this.sessionModalTab === t.id ? "active" : ""}" data-session-modal-tab="${t.id}">${t.label}</button>`).join("")}
                    </nav>
                    <div class="modal-body">
                        <form class="session-form" data-session-form>
                            <div class="modal-tab-panel ${this.sessionModalTab === "host" ? "active" : ""}">
                                <label><span>Host</span><input name="host" value="${escapeHtml(this.sessionForm.host)}" required /></label>
                                <label><span>Port</span><input name="port" type="number" value="${escapeHtml(this.sessionForm.port)}" min="1" required /></label>
                                <label>
                                    <span>Protocol</span>
                                    <select name="protocolId">
                                        <option value="ssh" ${this.sessionForm.protocolId === "ssh" ? "selected" : ""}>SSH</option>
                                        <option value="sftp" ${this.sessionForm.protocolId === "sftp" ? "selected" : ""}>SFTP</option>
                                    </select>
                                </label>
                            </div>
                            <div class="modal-tab-panel ${this.sessionModalTab === "auth" ? "active" : ""}">
                                <label><span>Username</span><input name="username" value="${escapeHtml(this.sessionForm.username)}" required /></label>
                                ${
                                  supportsKeyAuth
                                    ? `
                                <label>
                                    <span>Auth method</span>
                                    <select name="authMethod">
                                        <option value="password" ${this.sessionForm.authMethod === "password" ? "selected" : ""}>Password</option>
                                        <option value="key" ${this.sessionForm.authMethod === "key" ? "selected" : ""}>SSH key</option>
                                    </select>
                                </label>
                                `
                                    : ""
                                }
                                ${
                                  this.sessionForm.authMethod === "key" &&
                                  supportsKeyAuth
                                    ? `<label><span>Private key path</span><input name="privateKeyPath" value="${escapeHtml(this.sessionForm.privateKeyPath)}" placeholder="~/.ssh/id_ed25519" required /></label>
                                       <label><span>Key passphrase</span><input name="keyPassphrase" type="password" value="${escapeHtml(this.sessionForm.keyPassphrase)}" placeholder="Optional" /></label>
                                       ${isEditing && this.sessionForm.hasSavedKeyPassphrase ? '<div class="section-copy">Leave the key passphrase blank to keep the saved encrypted passphrase.</div>' : ""}`
                                    : `<label><span>Password</span><input name="password" type="password" value="${escapeHtml(this.sessionForm.password)}" /></label>
                                       ${isEditing && this.sessionForm.hasSavedPassword ? '<div class="section-copy">Leave the password blank to keep the saved encrypted password.</div>' : ""}`
                                }
                            </div>
                            <div class="modal-tab-panel ${this.sessionModalTab === "other" ? "active" : ""}">
                                <label><span>Name</span><input name="name" value="${escapeHtml(this.sessionForm.name)}" required /></label>
                                <label>
                                    <span>Tags</span>
                                    <div class="session-tag-editor">
                                        <div class="session-tag-list">
                                            ${this.sessionForm.tags
                                              .map(
                                                (tag) => `
                                                <span class="session-tag-chip">
                                                    ${escapeHtml(tag)}
                                                    <button type="button" class="session-tag-chip-remove" data-session-tag-remove="${escapeHtml(tag)}" aria-label="Remove tag ${escapeHtml(tag)}">×</button>
                                                </span>
                                            `,
                                              )
                                              .join("")}
                                        </div>
                                        <div class="session-tag-input-row">
                                            ${
                                              this.sessionTagInputVisible
                                                ? `<input data-session-tag-input placeholder="New tag" value="${escapeHtml(this.sessionTagDraft)}" />`
                                                : '<button type="button" class="action-button secondary" data-session-tag-add-open aria-label="Add tag">+ Add tag</button>'
                                            }
                                        </div>
                                    </div>
                                </label>
                                ${
                                  supportsSSHAdvancedOptions
                                    ? `
                                <label class="inline-check"><span>Use SSH agent</span><input name="useSSHAgent" type="checkbox" ${this.sessionForm.useSSHAgent ? "checked" : ""} /></label>
                                `
                                    : ""
                                }
                            </div>
                            <div class="modal-tab-panel ${this.sessionModalTab === "network" ? "active" : ""}">
                                ${
                                  supportsSSHAdvancedOptions
                                    ? `
                                <label><span>ProxyJump</span><input name="proxyJump" value="${escapeHtml(this.sessionForm.proxyJump)}" placeholder="bastion or user@bastion:22" /></label>
                                <label><span>Local tunnels</span><input name="localForwards" value="${escapeHtml(this.sessionForm.localForwards)}" placeholder="15432:db.internal:5432,18080:127.0.0.1:8080" /></label>
                                `
                                    : '<div class="empty-state">Network settings are available only for SSH and SFTP sessions.</div>'
                                }
                            </div>
                            <div style="padding: 0 1.5rem 1.25rem; display:flex; gap:0.75rem; justify-content:flex-end;">
                                <button type="button" class="action-button secondary" data-close-modal>Cancel</button>
                                <button type="submit" class="action-button">${isEditing ? "Update" : "Save"}</button>
                            </div>
                        </form>
                    </div>
                </div>
            </div>
        `;
  }

  private renderSessionContextMenu(): string {
    if (!this.sessionContextMenu.visible) {
      return "";
    }
    return `
            <div class="session-context-overlay" data-session-context-overlay>
                <div class="session-context-menu" data-session-context-menu style="left:${this.sessionContextMenu.x}px;top:${this.sessionContextMenu.y}px;">
                    <button class="session-context-item" data-session-context-open="${escapeHtml(this.sessionContextMenu.profileId)}">Open</button>
                    <button class="session-context-item" data-session-context-edit="${escapeHtml(this.sessionContextMenu.profileId)}">Edit settings</button>
                    <button class="session-context-item danger" data-session-context-delete="${escapeHtml(this.sessionContextMenu.profileId)}">Delete</button>
                </div>
            </div>
        `;
  }

  private hideSessionContextMenu(): void {
    if (!this.sessionContextMenu.visible) {
      return;
    }
    this.sessionContextMenu = { visible: false, x: 0, y: 0, profileId: "" };
    this.render();
  }

  private renderHostKeyDialog(): string {
    const { hostname, fingerprint } = this.hostKeyDialog;
    return `
            <div class="modal-overlay">
                <div class="modal-dialog">
                    <div class="panel-header compact-header">
                        <div>
                            <div class="eyebrow">Security alert</div>
                            <h2>Unknown host key</h2>
                        </div>
                    </div>
                    <div class="modal-body">
                        <p>The authenticity of host <strong>${escapeHtml(hostname)}</strong> cannot be established.</p>
                        <p>Key fingerprint:<br><code>${escapeHtml(fingerprint)}</code></p>
                        <p>Do you want to trust this host and add it to your known_hosts file?</p>
                        <div style="display:flex;gap:0.75rem;justify-content:flex-end;padding-top:1rem;">
                            <button class="action-button secondary" data-reject-host-key>Reject</button>
                            <button class="action-button" data-accept-host-key>Trust &amp; Connect</button>
                        </div>
                    </div>
                </div>
            </div>
        `;
  }

  private pickActiveTabID(preferredID: string): string {
    const tabs = this.shellState?.activeSessions ?? [];
    if (tabs.some((tab) => tab.id === preferredID)) {
      return preferredID;
    }
    return tabs[0]?.id ?? "";
  }

  private availableSessionInnerTabs(_protocolId: string): SessionInnerTab[] {
    return ["console", "sftp"];
  }

  private defaultSessionInnerTab(protocolId: string): SessionInnerTab {
    return this.availableSessionInnerTabs(protocolId)[0] ?? "console";
  }

  private loadStoredSessionInnerTabs(): Map<string, SessionInnerTab> {
    try {
      const stored = localStorage.getItem(SESSION_INNER_TABS_KEY);
      if (!stored) {
        return new Map<string, SessionInnerTab>();
      }
      const parsed = JSON.parse(stored) as Record<string, unknown>;
      return new Map<string, SessionInnerTab>(
        Object.entries(parsed).flatMap(([tabID, tabValue]) =>
          tabValue === "console" || tabValue === "sftp"
            ? [[tabID, tabValue]]
            : [],
        ),
      );
    } catch {
      return new Map<string, SessionInnerTab>();
    }
  }

  private persistSessionInnerTabs(): void {
    localStorage.setItem(
      SESSION_INNER_TABS_KEY,
      JSON.stringify(Object.fromEntries(this.sessionInnerTabs)),
    );
  }

  private pruneStoredSessionInnerTabs(): void {
    const activeSessions = this.shellState?.activeSessions ?? [];
    if (activeSessions.length === 0) {
      if (this.sessionInnerTabs.size > 0) {
        this.sessionInnerTabs.clear();
        this.persistSessionInnerTabs();
      }
      return;
    }
    const activeTabIDs = new Set(activeSessions.map((tab) => tab.id));
    let changed = false;
    for (const tabID of this.sessionInnerTabs.keys()) {
      if (activeTabIDs.has(tabID)) {
        continue;
      }
      this.sessionInnerTabs.delete(tabID);
      changed = true;
    }
    if (changed) {
      this.persistSessionInnerTabs();
    }
  }

  private storeSessionInnerTab(tabID: string, tab: SessionInnerTab): void {
    if (this.sessionInnerTabs.get(tabID) === tab) {
      return;
    }
    this.sessionInnerTabs.set(tabID, tab);
    this.persistSessionInnerTabs();
  }

  private restoreSessionInnerTab(tab: RuntimeSession): SessionInnerTab {
    const resolved = this.normalizeSessionInnerTab(
      tab.protocolId,
      this.sessionInnerTabs.get(tab.id) ??
        this.defaultSessionInnerTab(tab.protocolId),
    );
    this.storeSessionInnerTab(tab.id, resolved);
    return resolved;
  }

  private normalizeSessionInnerTab(
    protocolId: string,
    current: SessionInnerTab,
  ): SessionInnerTab {
    const availableTabs = this.availableSessionInnerTabs(protocolId);
    return availableTabs.includes(current)
      ? current
      : this.defaultSessionInnerTab(protocolId);
  }

  private renderSessionInnerTabs(): string {
    const activeTab = this.activeTab();
    if (!activeTab) {
      return "";
    }
    return this.availableSessionInnerTabs(activeTab.protocolId)
      .map(
        (tabID) =>
          `<button class="session-inner-tab ${this.sessionInnerTab === tabID ? "active" : ""}" data-session-inner-tab="${tabID}">${this.sessionInnerTabLabel(tabID)}</button>`,
      )
      .join("");
  }

  private sessionInnerTabLabel(tab: SessionInnerTab): string {
    return tab === "sftp" ? "SFTP" : "Console";
  }

  private activeTab(): RuntimeSession | null {
    return (
      this.shellState?.activeSessions.find(
        (tab) => tab.id === this.activeTabId,
      ) ?? null
    );
  }

  private selectedProvider(): AIProvider | null {
    return (
      this.shellState?.ai.providers.find((provider) => provider.selected) ??
      null
    );
  }

  private cloudProvider(): AIProvider | null {
    return (
      this.shellState?.ai.providers.find(
        (provider) => provider.id === "openai-compatible-cloud",
      ) ?? null
    );
  }

  private localProvider(): AIProvider | null {
    return (
      this.shellState?.ai.providers.find(
        (provider) => provider.id === "local-qwen3-4b",
      ) ?? null
    );
  }

  private commandPolicy(): CommandPolicyState {
    const raw = this.shellState?.ai.commandPolicy as
      aiModels.CommandPolicy | undefined;
    const tools = Array.isArray(raw?.tools) ? raw.tools : [];
    const commandRules = Array.isArray(raw?.commandRules)
      ? raw.commandRules
      : [];
    const pendingRequests = Array.isArray(raw?.pendingRequests)
      ? raw.pendingRequests
      : [];
    return {
      tools: tools.map((tool) => ({
        id: tool.id ?? "",
        name: tool.name ?? tool.id ?? "",
        description: tool.description ?? "",
        enabled: Boolean(tool.enabled),
      })),
      commandRules: commandRules.map((rule) => ({
        toolId: rule.toolId ?? "",
        sessionId: rule.sessionId ?? "",
        pattern: rule.pattern ?? "",
        action: rule.action ?? "ask",
        description: rule.description ?? "",
      })),
      pendingRequests: pendingRequests.map((request) => ({
        id: request.id ?? "",
        toolId: request.toolId ?? "",
        sessionId: request.sessionId ?? "",
        command: request.command ?? "",
        reason: request.reason ?? "",
        requestedAt: request.requestedAt ?? "",
      })),
      localDocsPath: raw?.localDocsPath ?? "",
    };
  }

  private async persistCommandPolicy(
    update: (policy: CommandPolicyState) => void,
  ): Promise<void> {
    await this.runAction(async () => {
      const policy = this.commandPolicy();
      update(policy);
      await UpdateCommandPolicy(policy as unknown as aiModels.CommandPolicy);
    }, "Unable to update command policy");
    await this.refresh("");
  }

  private currentLocalDownloadURL(): string {
    const fromInput =
      root?.querySelector<HTMLInputElement>("[data-local-model-url]")?.value ??
      "";
    return (
      fromInput ||
      this.localDraftDownloadURL ||
      this.localProvider()?.downloadUrl ||
      ""
    );
  }

  private hasConfiguredProvider(): boolean {
    return (this.shellState?.ai.providers ?? []).some(
      (provider) => provider.configured,
    );
  }

  private async ensureActiveSFTPLoaded(force = false): Promise<void> {
    if (this.sessionInnerTab !== "sftp") {
      return;
    }
    const activeTab = this.activeTab();
    if (!activeTab || activeTab.protocolId !== "ssh") {
      return;
    }
    const sameTab = this.sftpState.tabId === activeTab.id;
    if (
      !force &&
      sameTab &&
      (this.sftpState.loading ||
        this.sftpState.entries.length > 0 ||
        this.sftpState.error)
    ) {
      return;
    }
    const targetPath =
      sameTab && this.sftpState.path ? this.sftpState.path : "";
    await this.loadSFTP(activeTab.id, targetPath);
  }

  private syncSessionFormFromDOM(source?: EventTarget | null): void {
    const hostValue =
      root?.querySelector<HTMLInputElement>('input[name="host"]')?.value ??
      this.sessionForm.host;
    const nameInput =
      root?.querySelector<HTMLInputElement>('input[name="name"]');
    const nameValue = nameInput?.value ?? this.sessionForm.name;
    if (source instanceof HTMLInputElement && source.name === "name") {
      this.sessionNameAuto = !nameValue.trim();
    }
    this.sessionForm.host = hostValue;
    if (this.sessionNameAuto) {
      this.sessionForm.name = hostValue;
    } else {
      this.sessionForm.name = nameValue;
    }
    this.sessionForm.port =
      root?.querySelector<HTMLInputElement>('input[name="port"]')?.value ??
      this.sessionForm.port;
    this.sessionForm.username =
      root?.querySelector<HTMLInputElement>('input[name="username"]')?.value ??
      this.sessionForm.username;
    this.sessionForm.password =
      root?.querySelector<HTMLInputElement>('input[name="password"]')?.value ??
      this.sessionForm.password;
    this.sessionForm.keyPassphrase =
      root?.querySelector<HTMLInputElement>('input[name="keyPassphrase"]')
        ?.value ?? this.sessionForm.keyPassphrase;
    this.sessionForm.privateKeyPath =
      root?.querySelector<HTMLInputElement>('input[name="privateKeyPath"]')
        ?.value ?? this.sessionForm.privateKeyPath;
    const authMethodValue = root?.querySelector<HTMLSelectElement>(
      'select[name="authMethod"]',
    )?.value;
    this.sessionForm.authMethod =
      authMethodValue === "key"
        ? "key"
        : authMethodValue === "password"
          ? "password"
          : this.sessionForm.authMethod;
    this.sessionForm.protocolId =
      root?.querySelector<HTMLSelectElement>('select[name="protocolId"]')
        ?.value ?? this.sessionForm.protocolId;
    this.sessionForm.proxyJump =
      root?.querySelector<HTMLInputElement>('input[name="proxyJump"]')?.value ??
      this.sessionForm.proxyJump;
    this.sessionForm.localForwards =
      root?.querySelector<HTMLInputElement>('input[name="localForwards"]')
        ?.value ?? this.sessionForm.localForwards;
    this.sessionForm.useSSHAgent =
      root?.querySelector<HTMLInputElement>('input[name="useSSHAgent"]')
        ?.checked ?? this.sessionForm.useSSHAgent;
  }

  private supportsKeyAuth(_protocolId: string): boolean {
    return true;
  }

  private supportsSSHAdvancedOptions(protocolId: string): boolean {
    return protocolId === "ssh" || protocolId === "sftp";
  }

  private normalizeNotificationLevel(value?: string): NotificationLevel {
    if (value === "error" || value === "warn" || value === "debug") {
      return value;
    }
    return "info";
  }

  private pushNotification(
    level: NotificationLevel,
    message: string,
    time = new Date().toISOString(),
  ): void {
    const item: NotificationItem = {
      id: `notification-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`,
      level,
      message,
      time,
    };
    this.notifications = [item, ...this.notifications];
    this.toastQueue = [...this.toastQueue, item];
    window.setTimeout(() => {
      this.toastQueue = this.toastQueue.filter((entry) => entry.id !== item.id);
      this.render();
    }, 4500);
    this.render();
  }

  private setErrorMessage(message: string): void {
    this.errorMessage = message;
    if (message.trim()) {
      this.pushNotification("error", message);
    }
  }

  private focusFirstSidebarActionsMenuItem(): void {
    requestAnimationFrame(() => {
      root
        ?.querySelector<HTMLButtonElement>(".sidebar-actions-menu-item")
        ?.focus();
    });
  }

  private closeSidebarActionsMenu(focusTrigger = false): void {
    if (!this.showSidebarActionsMenu) {
      return;
    }
    this.showSidebarActionsMenu = false;
    this.render();
    if (focusTrigger) {
      requestAnimationFrame(() => {
        root
          ?.querySelector<HTMLButtonElement>(
            "[data-toggle-sidebar-actions-menu]",
          )
          ?.focus();
      });
    }
  }

  private initializeSettingsDrafts(): void {
    this.clearCloudAuthPolling();
    const provider = this.cloudProvider();
    const localProvider = this.localProvider();
    const shellSettings = this.shellState?.settings;
    this.cloudDraftModel = provider?.model ?? "";
    this.cloudDraftEndpoint = provider?.endpoint ?? "";
    this.cloudDraftToken = "";
    this.localDraftDownloadURL = localProvider?.downloadUrl ?? "";
    this.cloudAuthSessionId = "";
    this.cloudAuthPending = false;
    this.cloudAuthMessage = "";
    this.vaultDraftAddress = shellSettings?.vaultAddress ?? "";
    this.vaultDraftMountPoint = shellSettings?.vaultMountPoint ?? "secret";
    this.vaultDraftAuthMethod = normalizeVaultAuthMethod(
      shellSettings?.vaultAuthMethod,
    );
    this.vaultDraftLogin = shellSettings?.vaultLogin ?? "";
    this.vaultDraftToken = "";
    this.vaultDraftPassword = "";
    this.vaultDraftAutoRenewToken = shellSettings?.vaultAutoRenewToken ?? false;
    this.vaultDraftKeePassDatabasePath =
      shellSettings?.keepassDatabasePath ?? "";
    this.vaultDraftKeePassPassword = "";
  }

  private async promptForMasterPasswordOnStartup(): Promise<void> {
    if (this.startupMasterPasswordPrompted) {
      return;
    }
    this.startupMasterPasswordPrompted = true;
    let status: SecureStorageStatus;
    try {
      status = await GetSecureStorageStatus();
    } catch {
      return;
    }
    if (!status.available || status.configured) {
      return;
    }
    await this.requestMasterPassword(
      "create",
      false,
      "Create a master password to securely store passwords and tokens in the OS keychain-backed vault.",
    );
  }

  private async withMasterPasswordRetry<T>(
    action: () => Promise<T>,
    cancelMessage: string,
  ): Promise<T | undefined> {
    try {
      return await action();
    } catch (error) {
      if (!this.isMasterPasswordRequiredError(error)) {
        throw error;
      }
      const ready = await this.ensureMasterPasswordForLockedSecrets();
      if (!ready) {
        this.pushNotification("error", cancelMessage);
        return undefined;
      }
      return await action();
    }
  }

  private async ensureMasterPasswordForLockedSecrets(): Promise<boolean> {
    let status: SecureStorageStatus;
    try {
      status = await GetSecureStorageStatus();
    } catch (error) {
      this.pushNotification(
        "error",
        formatError("Unable to check secure storage status", error),
      );
      return false;
    }
    if (!status.available) {
      this.pushNotification(
        "error",
        "Secure storage is unavailable on this system, so saved passwords and tokens cannot be unlocked.",
      );
      return false;
    }
    if (status.unlocked) {
      return true;
    }
    const mode: MasterPasswordDialogMode = status.configured
      ? "unlock"
      : "create";
    const reason =
      mode === "create"
        ? "Create a master password to securely save passwords and tokens before continuing."
        : "Enter your master password to unlock saved passwords and tokens before continuing.";
    return this.requestMasterPassword(mode, true, reason);
  }

  private requestMasterPassword(
    mode: MasterPasswordDialogMode,
    required: boolean,
    reason: string,
  ): Promise<boolean> {
    return new Promise((resolve) => {
      this.masterPasswordDialog = {
        visible: true,
        mode,
        password: "",
        confirm: "",
        error: "",
        reason,
        required,
        resolver: resolve,
      };
      this.render();
      requestAnimationFrame(() => {
        root
          ?.querySelector<HTMLInputElement>("[data-master-password-input]")
          ?.focus();
      });
    });
  }

  private resolveMasterPasswordDialog(completed: boolean): void {
    const resolver = this.masterPasswordDialog.resolver;
    this.masterPasswordDialog = {
      visible: false,
      mode: "create",
      password: "",
      confirm: "",
      error: "",
      reason: "",
      required: false,
      resolver: null,
    };
    this.render();
    resolver?.(completed);
  }

  private renderMasterPasswordDialog(): string {
    const isCreate = this.masterPasswordDialog.mode === "create";
    return `
            <div class="modal-overlay">
                <div class="modal-dialog">
                    <div class="panel-header compact-header">
                        <div>
                            <div class="eyebrow">Secure storage</div>
                            <h2>${isCreate ? "Create master password" : "Unlock secure storage"}</h2>
                        </div>
                        ${this.masterPasswordDialog.required ? "" : '<button class="icon-button" data-master-password-cancel>×</button>'}
                    </div>
                    <div class="modal-body">
                        <form class="provider-form" data-master-password-form>
                            <div class="section-copy">${escapeHtml(this.masterPasswordDialog.reason)}</div>
                            <label>
                                <span>Master password</span>
                                <input type="password" data-master-password-input value="${escapeHtml(this.masterPasswordDialog.password)}" autocomplete="new-password" />
                            </label>
                            ${
                              isCreate
                                ? `
                                <label>
                                    <span>Confirm master password</span>
                                    <input type="password" data-master-password-confirm value="${escapeHtml(this.masterPasswordDialog.confirm)}" autocomplete="new-password" />
                                </label>
                            `
                                : ""
                            }
                            ${this.masterPasswordDialog.error ? `<div class="error-banner">${escapeHtml(this.masterPasswordDialog.error)}</div>` : ""}
                            <div class="provider-form-actions">
                                ${this.masterPasswordDialog.required ? "" : '<button class="action-button secondary" type="button" data-master-password-cancel>Later</button>'}
                                <button class="action-button" type="submit">${isCreate ? "Save master password" : "Unlock"}</button>
                            </div>
                        </form>
                    </div>
                </div>
            </div>
        `;
  }

  private isMasterPasswordRequiredError(error: unknown): boolean {
    const message = error instanceof Error ? error.message : String(error);
    return message.toLowerCase().includes("master password required");
  }

  private renderToasts(): string {
    if (this.toastQueue.length === 0) {
      return "";
    }
    return `
            <div class="toast-stack">
                ${this.toastQueue
                  .map(
                    (item) => `
                    <div class="toast toast-${item.level}">
                        <div class="toast-message">${escapeHtml(item.message)}</div>
                        <div class="toast-time">${escapeHtml(new Date(item.time).toLocaleTimeString())}</div>
                    </div>
                `,
                  )
                  .join("")}
            </div>
        `;
  }

  private renderNotificationCenter(): string {
    const rows =
      this.notifications.length > 0
        ? this.notifications
            .map(
              (item) => `
                <div class="notification-row notification-${item.level}">
                    <div class="notification-meta">
                        <span class="notification-level">${escapeHtml(item.level.toUpperCase())}</span>
                        <span class="notification-time">${escapeHtml(new Date(item.time).toLocaleString())}</span>
                    </div>
                    <div class="notification-message">${escapeHtml(item.message)}</div>
                    <button class="icon-button danger" data-delete-notification="${escapeHtml(item.id)}" title="Delete notification">✕</button>
                </div>
            `,
            )
            .join("")
        : '<div class="empty-state">No notifications yet.</div>';
    return `
            <div class="modal-overlay">
                <div class="modal-dialog wide notification-dialog">
                    <div class="panel-header compact-header">
                        <div>
                            <div class="eyebrow">Notifications</div>
                            <h2>Notification center</h2>
                        </div>
                        <div class="section-actions">
                            <button class="action-button secondary" data-clear-notifications ${this.notifications.length === 0 ? "disabled" : ""}>Clear all</button>
                            <button class="icon-button" data-close-modal>×</button>
                        </div>
                    </div>
                    <div class="modal-body notification-body">
                        <div class="notification-list">${rows}</div>
                    </div>
                </div>
            </div>
        `;
  }

  private async runAction(
    action: () => Promise<void>,
    prefix: string,
  ): Promise<void> {
    try {
      const completed = await this.withMasterPasswordRetry(
        action,
        `${prefix}: master password setup was cancelled.`,
      );
      if (typeof completed === "undefined") {
        return;
      }
      await this.refresh("");
    } catch (error) {
      this.setErrorMessage(formatError(prefix, error));
      this.render();
    }
  }

  private defaultSessionForm(): SessionFormState {
    return {
      name: "",
      host: "",
      port: "22",
      username: "",
      password: "",
      keyPassphrase: "",
      authMethod: "password",
      privateKeyPath: "",
      protocolId: "ssh",
      tags: [],
      proxyJump: "",
      localForwards: "",
      useSSHAgent: false,
      hasSavedPassword: false,
      hasSavedKeyPassphrase: false,
    };
  }

  private defaultSFTPState(tabID: string | null): SFTPState {
    return {
      tabId: tabID,
      path: "",
      entries: [],
      loading: false,
      error: "",
      editorOpen: false,
      editorPath: "",
      editorContent: "",
      editorLoading: false,
      editorSaving: false,
      editorDirty: false,
      editorError: "",
      selectedFiles: [],
    };
  }

  private allSessionTags(): string[] {
    const tags = new Set<string>();
    for (const profile of this.shellState?.sessionProfiles ?? []) {
      for (const tag of profile.tags ?? []) {
        const normalized = String(tag ?? "").trim();
        if (normalized) {
          tags.add(normalized);
        }
      }
    }
    return [...tags].sort((left, right) => left.localeCompare(right));
  }

  private reconcileSelectedSessionTags(): void {
    const allTags = this.allSessionTags();
    const hasUntagged = (this.shellState?.sessionProfiles ?? []).some(
      (profile) => (profile.tags ?? []).length === 0,
    );
    const filterKeys = hasUntagged
      ? [...allTags, this.untaggedFilterTag]
      : allTags;
    if (filterKeys.length === 0) {
      this.selectedSessionTags = new Set<string>();
      this.knownSessionTags = new Set<string>();
      this.sessionTagFilterInitialized = false;
      return;
    }
    if (!this.sessionTagFilterInitialized) {
      this.selectedSessionTags = new Set(filterKeys);
      this.knownSessionTags = new Set(filterKeys);
      this.sessionTagFilterInitialized = true;
      return;
    }
    const next = new Set<string>();
    for (const tag of filterKeys) {
      if (this.selectedSessionTags.has(tag)) {
        next.add(tag);
      }
    }
    for (const tag of filterKeys) {
      if (!this.knownSessionTags.has(tag)) {
        next.add(tag);
      }
    }
    this.selectedSessionTags = next;
    this.knownSessionTags = new Set(filterKeys);
  }

  private toggleSessionTagFilter(tag: string): void {
    if (this.selectedSessionTags.has(tag)) {
      this.selectedSessionTags.delete(tag);
    } else {
      this.selectedSessionTags.add(tag);
    }
    this.render();
  }

  private filteredSessionProfiles(): sessions.Profile[] {
    const profiles = this.shellState?.sessionProfiles ?? [];
    const allTags = this.allSessionTags();
    const hasUntagged = profiles.some(
      (profile) => (profile.tags ?? []).length === 0,
    );
    const filterKeys = hasUntagged
      ? [...allTags, this.untaggedFilterTag]
      : allTags;
    const allSelected = filterKeys.every((tag) =>
      this.selectedSessionTags.has(tag),
    );
    if (filterKeys.length === 0 || allSelected) {
      return profiles;
    }
    return profiles.filter((profile) => {
      const tags = profile.tags ?? [];
      if (tags.length === 0) {
        return this.selectedSessionTags.has(this.untaggedFilterTag);
      }
      return tags.some((tag) => this.selectedSessionTags.has(tag));
    });
  }

  private commitSessionTagDraft(): void {
    const tag = this.sessionTagDraft.trim();
    if (tag && !this.sessionForm.tags.includes(tag)) {
      this.sessionForm.tags = [...this.sessionForm.tags, tag];
    }
    this.sessionTagDraft = "";
    this.sessionTagInputVisible = false;
  }
}

function inferDirectory(entries: FileEntry[], requestedPath: string): string {
  if (requestedPath) {
    return requestedPath;
  }
  const firstEntry = entries[0];
  if (!firstEntry) {
    return ".";
  }
  return parentPath(firstEntry.path);
}

function parentPath(value: string): string {
  if (!value || value === "." || value === "/") {
    return ".";
  }
  const normalized = value.endsWith("/") ? value.slice(0, -1) : value;
  const index = normalized.lastIndexOf("/");
  if (index <= 0) {
    return ".";
  }
  return normalized.slice(0, index);
}

function normalizeVaultAuthMethod(value: unknown): VaultAuthMethod {
  const normalized = String(value ?? "")
    .trim()
    .toLowerCase();
  if (
    normalized === "oidc" ||
    normalized === "oidc-sec" ||
    normalized === "domain"
  ) {
    return normalized;
  }
  return "token";
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB"];
  let size = value;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex++;
  }
  return `${size.toFixed(size >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function escapeClassName(value: string): string {
  return value.replace(/[^a-zA-Z0-9_-]/g, "-");
}

function formatError(prefix: string, error: unknown): string {
  if (error instanceof Error) {
    return `${prefix}: ${error.message}`;
  }
  if (typeof error === "string") {
    return `${prefix}: ${error}`;
  }
  return prefix;
}

function renderMarkdown(value: string): string {
  const placeholders: string[] = [];
  let escaped = escapeHtml(value).replaceAll("\r\n", "\n");
  escaped = escaped.replace(
    /```(?:[^\n`]*)\n([\s\S]*?)```/g,
    (_match, code: string) => {
      const index =
        placeholders.push(
          `<pre class="md-block-code"><code>${code.replace(/\n+$/g, "")}</code></pre>`,
        ) - 1;
      return `@@MD_CODE_BLOCK_${index}@@`;
    },
  );

  const lines = escaped.split("\n");
  const blocks: string[] = [];
  let inList = false;

  const closeList = () => {
    if (inList) {
      blocks.push("</ul>");
      inList = false;
    }
  };

  for (const rawLine of lines) {
    const line = rawLine.trimEnd();
    const trimmed = line.trim();
    if (!trimmed) {
      closeList();
      continue;
    }
    const codePlaceholder = trimmed.match(/^@@MD_CODE_BLOCK_(\d+)@@$/);
    if (codePlaceholder) {
      closeList();
      blocks.push(trimmed);
      continue;
    }
    const heading = trimmed.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      closeList();
      const level = heading[1].length;
      blocks.push(`<h${level}>${applyInlineMarkdown(heading[2])}</h${level}>`);
      continue;
    }
    const quote = trimmed.match(/^>\s?(.*)$/);
    if (quote) {
      closeList();
      blocks.push(`<blockquote>${applyInlineMarkdown(quote[1])}</blockquote>`);
      continue;
    }
    const listItem = trimmed.match(/^[-*]\s+(.+)$/);
    if (listItem) {
      if (!inList) {
        blocks.push("<ul>");
        inList = true;
      }
      blocks.push(`<li>${applyInlineMarkdown(listItem[1])}</li>`);
      continue;
    }
    closeList();
    blocks.push(`<p>${applyInlineMarkdown(trimmed)}</p>`);
  }
  closeList();

  let html = blocks.join("");
  html = html.replace(
    /@@MD_CODE_BLOCK_(\d+)@@/g,
    (_match, idx: string) => placeholders[Number(idx)] ?? "",
  );
  return html;
}

function applyInlineMarkdown(value: string): string {
  return value
    .replace(
      /\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)/g,
      '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>',
    )
    .replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>")
    .replace(/\*([^*\n]+)\*/g, "<em>$1</em>")
    .replace(/`([^`\n]+)`/g, '<code class="md-inline-code">$1</code>');
}

void new EiksyShell().bootstrap();
