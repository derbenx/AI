export namespace main {

	export class Config {
	    model_path: string;
	    clip_path: string;
	    personality: string;
	    memory_limit: number;
	    remember_first: boolean;
	    debug_log: boolean;
	    gpu_layers: number;

	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model_path = source["model_path"];
	        this.clip_path = source["clip_path"];
	        this.personality = source["personality"];
	        this.memory_limit = source["memory_limit"];
	        this.remember_first = source["remember_first"];
	        this.debug_log = source["debug_log"];
	        this.gpu_layers = source["gpu_layers"];
	    }
	}
	export class SystemSpecs {
	    cpu: string;
	    ram: number;
	    vram: number;
	    gpu: string;
	    os: string;
	    balanced_gpu: number;

	    static createFrom(source: any = {}) {
	        return new SystemSpecs(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cpu = source["cpu"];
	        this.ram = source["ram"];
	        this.vram = source["vram"];
	        this.gpu = source["gpu"];
	        this.os = source["os"];
	        this.balanced_gpu = source["balanced_gpu"];
	    }
	}

}
