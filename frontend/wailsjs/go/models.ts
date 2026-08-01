export namespace activity {
	
	export class Entry {
	    ts: string;
	    level: string;
	    scope: string;
	    message: string;
	    job_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ts = source["ts"];
	        this.level = source["level"];
	        this.scope = source["scope"];
	        this.message = source["message"];
	        this.job_id = source["job_id"];
	    }
	}

}

export namespace config {
	
	export class UI {
	    theme: string;
	    default_mode: string;
	    default_download: string;
	
	    static createFrom(source: any = {}) {
	        return new UI(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.default_mode = source["default_mode"];
	        this.default_download = source["default_download"];
	    }
	}
	export class Network {
	    max_concurrent_downloads: number;
	    verify_tls: boolean;
	    request_timeout_seconds: number;
	    retry_count: number;
	
	    static createFrom(source: any = {}) {
	        return new Network(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.max_concurrent_downloads = source["max_concurrent_downloads"];
	        this.verify_tls = source["verify_tls"];
	        this.request_timeout_seconds = source["request_timeout_seconds"];
	        this.retry_count = source["retry_count"];
	    }
	}
	export class Storage {
	    library_dir: string;
	
	    static createFrom(source: any = {}) {
	        return new Storage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.library_dir = source["library_dir"];
	    }
	}
	export class RPCS3 {
	    games_yml: string;
	    hdd0_game: string;
	
	    static createFrom(source: any = {}) {
	        return new RPCS3(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.games_yml = source["games_yml"];
	        this.hdd0_game = source["hdd0_game"];
	    }
	}
	export class Config {
	    schema_version: number;
	    rpcs3: RPCS3;
	    storage: Storage;
	    network: Network;
	    ui: UI;
	    home_dir: string;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.rpcs3 = this.convertValues(source["rpcs3"], RPCS3);
	        this.storage = this.convertValues(source["storage"], Storage);
	        this.network = this.convertValues(source["network"], Network);
	        this.ui = this.convertValues(source["ui"], UI);
	        this.home_dir = source["home_dir"];
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

export namespace downloads {
	
	export class File {
	    path: string;
	    name: string;
	    size: number;
	    version?: string;
	    kind?: string;
	
	    static createFrom(source: any = {}) {
	        return new File(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.size = source["size"];
	        this.version = source["version"];
	        this.kind = source["kind"];
	    }
	}
	export class Title {
	    mode: string;
	    title_id: string;
	    path: string;
	    file_count: number;
	    total_size: number;
	    latest_version?: string;
	    files: File[];
	
	    static createFrom(source: any = {}) {
	        return new Title(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.title_id = source["title_id"];
	        this.path = source["path"];
	        this.file_count = source["file_count"];
	        this.total_size = source["total_size"];
	        this.latest_version = source["latest_version"];
	        this.files = this.convertValues(source["files"], File);
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

export namespace jobs {
	
	export class Job {
	    id: string;
	    title_id: string;
	    title_name?: string;
	    mode: string;
	    kind?: string;
	    update: psn.Update;
	    dest_path: string;
	    state: string;
	    downloaded: number;
	    error?: string;
	    installed_to?: string;
	    throughput?: number;
	    eta?: number;
	    attempt?: number;
	    max_attempts?: number;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title_id = source["title_id"];
	        this.title_name = source["title_name"];
	        this.mode = source["mode"];
	        this.kind = source["kind"];
	        this.update = this.convertValues(source["update"], psn.Update);
	        this.dest_path = source["dest_path"];
	        this.state = source["state"];
	        this.downloaded = source["downloaded"];
	        this.error = source["error"];
	        this.installed_to = source["installed_to"];
	        this.throughput = source["throughput"];
	        this.eta = source["eta"];
	        this.attempt = source["attempt"];
	        this.max_attempts = source["max_attempts"];
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
	export class Request {
	    title_id: string;
	    title_name?: string;
	    mode: string;
	    kind?: string;
	    update: psn.Update;
	
	    static createFrom(source: any = {}) {
	        return new Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title_id = source["title_id"];
	        this.title_name = source["title_name"];
	        this.mode = source["mode"];
	        this.kind = source["kind"];
	        this.update = this.convertValues(source["update"], psn.Update);
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

export namespace library {
	
	export class Row {
	    title_id: string;
	    name?: string;
	    install_dir: string;
	    status: string;
	    installed_version?: string;
	    latest_local?: string;
	    latest_server?: string;
	    updates?: psn.Update[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new Row(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title_id = source["title_id"];
	        this.name = source["name"];
	        this.install_dir = source["install_dir"];
	        this.status = source["status"];
	        this.installed_version = source["installed_version"];
	        this.latest_local = source["latest_local"];
	        this.latest_server = source["latest_server"];
	        this.updates = this.convertValues(source["updates"], psn.Update);
	        this.error = source["error"];
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

export namespace psn {
	
	export class FirmwareEntry {
	    locale: string;
	    type?: string;
	    version: string;
	    url: string;
	    size?: number;
	    sha1sum?: string;
	
	    static createFrom(source: any = {}) {
	        return new FirmwareEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.locale = source["locale"];
	        this.type = source["type"];
	        this.version = source["version"];
	        this.url = source["url"];
	        this.size = source["size"];
	        this.sha1sum = source["sha1sum"];
	    }
	}
	export class FirmwareList {
	    console: string;
	    entries: FirmwareEntry[];
	
	    static createFrom(source: any = {}) {
	        return new FirmwareList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.console = source["console"];
	        this.entries = this.convertValues(source["entries"], FirmwareEntry);
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
	export class Update {
	    version: string;
	    size: number;
	    sha1sum: string;
	    url: string;
	    system_version?: string;
	    drm_type?: string;
	
	    static createFrom(source: any = {}) {
	        return new Update(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.size = source["size"];
	        this.sha1sum = source["sha1sum"];
	        this.url = source["url"];
	        this.system_version = source["system_version"];
	        this.drm_type = source["drm_type"];
	    }
	}
	export class Title {
	    id: string;
	    name?: string;
	    updates: Update[];
	
	    static createFrom(source: any = {}) {
	        return new Title(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.updates = this.convertValues(source["updates"], Update);
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

export namespace rpcs3 {
	
	export class Entry {
	    title_id: string;
	    install_dir: string;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title_id = source["title_id"];
	        this.install_dir = source["install_dir"];
	    }
	}

}

