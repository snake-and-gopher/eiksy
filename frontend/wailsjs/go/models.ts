export namespace ai {
	
	export class ChatMessage {
	    role: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	    }
	}
	export class ContextPolicy {
	    sendTerminalSelection: boolean;
	    sendRecentOutput: boolean;
	    requireConfirmation: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ContextPolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sendTerminalSelection = source["sendTerminalSelection"];
	        this.sendRecentOutput = source["sendRecentOutput"];
	        this.requireConfirmation = source["requireConfirmation"];
	    }
	}
	export class CommandRequest {
	    id: string;
	    toolId: string;
	    sessionId: string;
	    command: string;
	    reason?: string;
	    requestedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new CommandRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.toolId = source["toolId"];
	        this.sessionId = source["sessionId"];
	        this.command = source["command"];
	        this.reason = source["reason"];
	        this.requestedAt = source["requestedAt"];
	    }
	}
	export class CommandTool {
	    id: string;
	    name: string;
	    description?: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CommandTool(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.enabled = source["enabled"];
	    }
	}
	export class CommandRule {
	    toolId?: string;
	    sessionId?: string;
	    pattern: string;
	    action: string;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new CommandRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.toolId = source["toolId"];
	        this.sessionId = source["sessionId"];
	        this.pattern = source["pattern"];
	        this.action = source["action"];
	        this.description = source["description"];
	    }
	}
	export class CommandPolicy {
	    tools: CommandTool[];
	    commandRules: CommandRule[];
	    pendingRequests: CommandRequest[];
	    localDocsPath?: string;
	
	    static createFrom(source: any = {}) {
	        return new CommandPolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tools = this.convertValues(source["tools"], CommandTool);
	        this.commandRules = this.convertValues(source["commandRules"], CommandRule);
	        this.pendingRequests = this.convertValues(source["pendingRequests"], CommandRequest);
	        this.localDocsPath = source["localDocsPath"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProviderDescriptor {
	    id: string;
	    name: string;
	    class: string;
	    model: string;
	    endpoint?: string;
	    downloadUrl?: string;
	    localPath?: string;
	    status: string;
	    selected: boolean;
	    hasToken: boolean;
	    configured: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderDescriptor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.class = source["class"];
	        this.model = source["model"];
	        this.endpoint = source["endpoint"];
	        this.downloadUrl = source["downloadUrl"];
	        this.localPath = source["localPath"];
	        this.status = source["status"];
	        this.selected = source["selected"];
	        this.hasToken = source["hasToken"];
	        this.configured = source["configured"];
	    }
	}
	export class WorkspaceState {
	    providers: ProviderDescriptor[];
	    contextPolicy: ContextPolicy;
	    commandPolicy: CommandPolicy;
	    messages: ChatMessage[];
	    agentMessages: ChatMessage[];
	    chatSessionId: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providers = this.convertValues(source["providers"], ProviderDescriptor);
	        this.contextPolicy = this.convertValues(source["contextPolicy"], ContextPolicy);
	        this.commandPolicy = this.convertValues(source["commandPolicy"], CommandPolicy);
	        this.messages = this.convertValues(source["messages"], ChatMessage);
	        this.agentMessages = this.convertValues(source["agentMessages"], ChatMessage);
	        this.chatSessionId = source["chatSessionId"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace app {
	
	export class CloudProviderAuthSession {
	    id: string;
	    status: string;
	    authUrl?: string;
	    message?: string;
	    endpoint?: string;
	
	    static createFrom(source: any = {}) {
	        return new CloudProviderAuthSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.status = source["status"];
	        this.authUrl = source["authUrl"];
	        this.message = source["message"];
	        this.endpoint = source["endpoint"];
	    }
	}
	export class RuntimeSessionView {
	    id: string;
	    title: string;
	    protocolId: string;
	    profileId: string;
	    status: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeSessionView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.protocolId = source["protocolId"];
	        this.profileId = source["profileId"];
	        this.status = source["status"];
	        this.description = source["description"];
	    }
	}
	export class WorkspaceView {
	    layout: workspace.Layout;
	    recentEvents: workspace.Event[];
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.layout = this.convertValues(source["layout"], workspace.Layout);
	        this.recentEvents = this.convertValues(source["recentEvents"], workspace.Event);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ShellState {
	    protocols: protocols.Descriptor[];
	    sessionProfiles: sessions.Profile[];
	    activeSessions: RuntimeSessionView[];
	    sessionHistory: sessions.HistoryEntry[];
	    ai: ai.WorkspaceState;
	    workspace: WorkspaceView;
	    settings: settings.AppSettings;
	
	    static createFrom(source: any = {}) {
	        return new ShellState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocols = this.convertValues(source["protocols"], protocols.Descriptor);
	        this.sessionProfiles = this.convertValues(source["sessionProfiles"], sessions.Profile);
	        this.activeSessions = this.convertValues(source["activeSessions"], RuntimeSessionView);
	        this.sessionHistory = this.convertValues(source["sessionHistory"], sessions.HistoryEntry);
	        this.ai = this.convertValues(source["ai"], ai.WorkspaceState);
	        this.workspace = this.convertValues(source["workspace"], WorkspaceView);
	        this.settings = this.convertValues(source["settings"], settings.AppSettings);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace protocols {
	
	export class Descriptor {
	    id: string;
	    name: string;
	    scheme: string;
	    capabilities: string[];
	
	    static createFrom(source: any = {}) {
	        return new Descriptor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.scheme = source["scheme"];
	        this.capabilities = source["capabilities"];
	    }
	}

}

export namespace sessions {
	
	export class HistoryEntry {
	    profileId: string;
	    profileName: string;
	    launchedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profileId = source["profileId"];
	        this.profileName = source["profileName"];
	        this.launchedAt = source["launchedAt"];
	    }
	}
	export class Profile {
	    id: string;
	    name: string;
	    group: string;
	    tags: string[];
	    favorite: boolean;
	    protocolId: string;
	    host: string;
	    port: number;
	    username: string;
	    hasPassword: boolean;
	    hasKeyPassphrase: boolean;
	    secretRef?: string;
	    options?: Record<string, string>;
	    lastLaunchedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.group = source["group"];
	        this.tags = source["tags"];
	        this.favorite = source["favorite"];
	        this.protocolId = source["protocolId"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.hasPassword = source["hasPassword"];
	        this.hasKeyPassphrase = source["hasKeyPassphrase"];
	        this.secretRef = source["secretRef"];
	        this.options = source["options"];
	        this.lastLaunchedAt = source["lastLaunchedAt"];
	    }
	}
	export class ProfileInput {
	    id: string;
	    name: string;
	    group: string;
	    tags: string[];
	    favorite: boolean;
	    protocolId: string;
	    host: string;
	    port: number;
	    username: string;
	    password?: string;
	    keyPassphrase?: string;
	    secretRef?: string;
	    options?: Record<string, string>;
	    lastLaunchedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProfileInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.group = source["group"];
	        this.tags = source["tags"];
	        this.favorite = source["favorite"];
	        this.protocolId = source["protocolId"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.keyPassphrase = source["keyPassphrase"];
	        this.secretRef = source["secretRef"];
	        this.options = source["options"];
	        this.lastLaunchedAt = source["lastLaunchedAt"];
	    }
	}

}

export namespace settings {
	
	export class WindowLayout {
	    sidebarWidth: number;
	    assistantWidth: number;
	
	    static createFrom(source: any = {}) {
	        return new WindowLayout(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sidebarWidth = source["sidebarWidth"];
	        this.assistantWidth = source["assistantWidth"];
	    }
	}
	export class WindowState {
	    width: number;
	    height: number;
	    x: number;
	    y: number;
	    maximized: boolean;
	    saved: boolean;

	    static createFrom(source: any = {}) {
	        return new WindowState(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.width = source["width"];
	        this.height = source["height"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.maximized = source["maximized"];
	        this.saved = source["saved"];
	    }
	}
	export class PortForwardRule {
	    localPort: string;
	    remoteHost: string;
	    remotePort: string;
	    hostId: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PortForwardRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.localPort = source["localPort"];
	        this.remoteHost = source["remoteHost"];
	        this.remotePort = source["remotePort"];
	        this.hostId = source["hostId"];
	        this.enabled = source["enabled"];
	    }
	}
	export class AppSettings {
	    theme: string;
	    defaultProtocol: string;
	    windowLayout: WindowLayout;
	    windowState: WindowState;
	    promptBeforeAi: boolean;
	    allowCloudModels: boolean;
	    portForwardRules: PortForwardRule[];
	    sshConfigAutoLoaded: boolean;
	    vaultAddress: string;
	    vaultMountPoint: string;
	    vaultAutoRenewToken: boolean;
	    vaultAuthMethod: string;
	    vaultLogin: string;
	    vaultProvider: string;
	    keepassDatabasePath: string;
	    keepassPassword: string;
	    vaultToken: string;
	    vaultPassword: string;
	    hasKeePassPassword: boolean;
	    hasVaultToken: boolean;
	    hasVaultPassword: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.defaultProtocol = source["defaultProtocol"];
	        this.windowLayout = this.convertValues(source["windowLayout"], WindowLayout);
	        this.windowState = this.convertValues(source["windowState"], WindowState);
	        this.promptBeforeAi = source["promptBeforeAi"];
	        this.allowCloudModels = source["allowCloudModels"];
	        this.portForwardRules = this.convertValues(source["portForwardRules"], PortForwardRule);
	        this.sshConfigAutoLoaded = source["sshConfigAutoLoaded"];
	        this.vaultAddress = source["vaultAddress"];
	        this.vaultMountPoint = source["vaultMountPoint"];
	        this.vaultAutoRenewToken = source["vaultAutoRenewToken"];
	        this.vaultAuthMethod = source["vaultAuthMethod"];
	        this.vaultLogin = source["vaultLogin"];
	        this.vaultProvider = source["vaultProvider"];
	        this.keepassDatabasePath = source["keepassDatabasePath"];
	        this.keepassPassword = source["keepassPassword"];
	        this.vaultToken = source["vaultToken"];
	        this.vaultPassword = source["vaultPassword"];
	        this.hasKeePassPassword = source["hasKeePassPassword"];
	        this.hasVaultToken = source["hasVaultToken"];
	        this.hasVaultPassword = source["hasVaultPassword"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace sftp {
	
	export class FileEntry {
	    name: string;
	    path: string;
	    isDir: boolean;
	    size: number;
	    modTime: string;
	    mode: string;
	
	    static createFrom(source: any = {}) {
	        return new FileEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.size = source["size"];
	        this.modTime = source["modTime"];
	        this.mode = source["mode"];
	    }
	}

}

export namespace workspace {
	
	export class Event {
	    id: string;
	    type: string;
	    subject: string;
	    at: string;
	
	    static createFrom(source: any = {}) {
	        return new Event(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.subject = source["subject"];
	        this.at = source["at"];
	    }
	}
	export class SidebarSection {
	    id: string;
	    title: string;
	
	    static createFrom(source: any = {}) {
	        return new SidebarSection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	    }
	}
	export class Layout {
	    sidebarSections: SidebarSection[];
	    activeTabId?: string;
	
	    static createFrom(source: any = {}) {
	        return new Layout(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sidebarSections = this.convertValues(source["sidebarSections"], SidebarSection);
	        this.activeTabId = source["activeTabId"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}


export namespace securestorage {
	
	export class Status {
	    available: boolean;
	    configured: boolean;
	    unlocked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.configured = source["configured"];
	        this.unlocked = source["unlocked"];
	    }
	}

}
