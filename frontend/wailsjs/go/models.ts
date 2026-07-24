export namespace main {
	
	export class BootstrapState {
	    entries: model.Entry[];
	    categories: string[];
	    profiles: profile.Profile[];
	    activeProfile?: profile.Profile;
	    vault: vault.Status;
	    shortcuts: vault.Shortcut[];
	    secrets: vault.SecretMeta[];
	    libraryDir: string;
	    profileRoot: string;
	    vaultFile?: string;
	    hotkeyAvailable: boolean;
	    hotkeyMessage: string;
	    logFile: string;
	    initError?: string;
	
	    static createFrom(source: any = {}) {
	        return new BootstrapState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entries = this.convertValues(source["entries"], model.Entry);
	        this.categories = source["categories"];
	        this.profiles = this.convertValues(source["profiles"], profile.Profile);
	        this.activeProfile = this.convertValues(source["activeProfile"], profile.Profile);
	        this.vault = this.convertValues(source["vault"], vault.Status);
	        this.shortcuts = this.convertValues(source["shortcuts"], vault.Shortcut);
	        this.secrets = this.convertValues(source["secrets"], vault.SecretMeta);
	        this.libraryDir = source["libraryDir"];
	        this.profileRoot = source["profileRoot"];
	        this.vaultFile = source["vaultFile"];
	        this.hotkeyAvailable = source["hotkeyAvailable"];
	        this.hotkeyMessage = source["hotkeyMessage"];
	        this.logFile = source["logFile"];
	        this.initError = source["initError"];
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

export namespace model {
	
	export class Variable {
	    name: string;
	    label: string;
	    type: string;
	    default?: string;
	    placeholder?: string;
	    format?: string;
	    required?: boolean;
	    options?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Variable(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.type = source["type"];
	        this.default = source["default"];
	        this.placeholder = source["placeholder"];
	        this.format = source["format"];
	        this.required = source["required"];
	        this.options = source["options"];
	    }
	}
	export class Entry {
	    id: string;
	    trigger: string;
	    title: string;
	    description?: string;
	    category: string;
	    template: string;
	    variables?: Variable[];
	    tags?: string[];
	    platform?: string;
	    project?: string;
	    environment?: string;
	    riskLevel: string;
	    favorite?: boolean;
	    updatedAt?: string;
	    sourceFile?: string;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.trigger = source["trigger"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.category = source["category"];
	        this.template = source["template"];
	        this.variables = this.convertValues(source["variables"], Variable);
	        this.tags = source["tags"];
	        this.platform = source["platform"];
	        this.project = source["project"];
	        this.environment = source["environment"];
	        this.riskLevel = source["riskLevel"];
	        this.favorite = source["favorite"];
	        this.updatedAt = source["updatedAt"];
	        this.sourceFile = source["sourceFile"];
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

export namespace profile {
	
	export class Profile {
	    id: string;
	    name: string;
	    createdAt: string;
	    lastUsedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.createdAt = source["createdAt"];
	        this.lastUsedAt = source["lastUsedAt"];
	    }
	}

}

export namespace vault {
	
	export class Secret {
	    id: string;
	    name: string;
	    username?: string;
	    value: string;
	    notes?: string;
	    tags?: string[];
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Secret(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.username = source["username"];
	        this.value = source["value"];
	        this.notes = source["notes"];
	        this.tags = source["tags"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class SecretMeta {
	    id: string;
	    name: string;
	    username?: string;
	    notes?: string;
	    tags?: string[];
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SecretMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.username = source["username"];
	        this.notes = source["notes"];
	        this.tags = source["tags"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class Shortcut {
	    id: string;
	    trigger: string;
	    title: string;
	    category?: string;
	    kind?: string;
	    template?: string;
	    variables?: model.Variable[];
	    fields?: Record<string, string>;
	    content: string;
	    secretId?: string;
	    sensitive?: boolean;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Shortcut(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.trigger = source["trigger"];
	        this.title = source["title"];
	        this.category = source["category"];
	        this.kind = source["kind"];
	        this.template = source["template"];
	        this.variables = this.convertValues(source["variables"], model.Variable);
	        this.fields = source["fields"];
	        this.content = source["content"];
	        this.secretId = source["secretId"];
	        this.sensitive = source["sensitive"];
	        this.updatedAt = source["updatedAt"];
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
	export class Status {
	    exists: boolean;
	    unlocked: boolean;
	    autoLockSeconds: number;
	    remainingSeconds: number;
	    storedSecretCount: number;
	    storedShortcutCount: number;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exists = source["exists"];
	        this.unlocked = source["unlocked"];
	        this.autoLockSeconds = source["autoLockSeconds"];
	        this.remainingSeconds = source["remainingSeconds"];
	        this.storedSecretCount = source["storedSecretCount"];
	        this.storedShortcutCount = source["storedShortcutCount"];
	    }
	}

}

