export namespace main {
	
	export class RecorderConfig {
	    baseUrl: string;
	    sourceKey: string;
	    timeoutSec: number;
	    device: string;
	    sampleRate: number;
	    channels: number;
	    blockMs: number;
	    threshold: number;
	    silenceMs: number;
	    minDurationMs: number;
	    maxDurationSec: number;
	    preRollMs: number;
	    system: number;
	    systemLabel: string;
	    talkgroup: number;
	    talkgroupLabel: string;
	    talkgroupGroup: string;
	    talkgroupTag: string;
	    frequency: number;
	    queueDirectory: string;
	    folderIngestEnabled: boolean;
	    folderIngestDirectory: string;
	    folderIngestProcessedDirectory: string;
	    folderIngestReprocessProcessed: boolean;
	    folderIngestPollMs: number;
	    folderIngestStableMs: number;
	    canaryEnabled: boolean;
	    canaryIntervalSec: number;
	    canaryTalkgroup: number;
	    canaryTalkgroupLabel: string;
	    configPath: string;
	
	    static createFrom(source: any = {}) {
	        return new RecorderConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.sourceKey = source["sourceKey"];
	        this.timeoutSec = source["timeoutSec"];
	        this.device = source["device"];
	        this.sampleRate = source["sampleRate"];
	        this.channels = source["channels"];
	        this.blockMs = source["blockMs"];
	        this.threshold = source["threshold"];
	        this.silenceMs = source["silenceMs"];
	        this.minDurationMs = source["minDurationMs"];
	        this.maxDurationSec = source["maxDurationSec"];
	        this.preRollMs = source["preRollMs"];
	        this.system = source["system"];
	        this.systemLabel = source["systemLabel"];
	        this.talkgroup = source["talkgroup"];
	        this.talkgroupLabel = source["talkgroupLabel"];
	        this.talkgroupGroup = source["talkgroupGroup"];
	        this.talkgroupTag = source["talkgroupTag"];
	        this.frequency = source["frequency"];
	        this.queueDirectory = source["queueDirectory"];
	        this.folderIngestEnabled = source["folderIngestEnabled"];
	        this.folderIngestDirectory = source["folderIngestDirectory"];
	        this.folderIngestProcessedDirectory = source["folderIngestProcessedDirectory"];
	        this.folderIngestReprocessProcessed = source["folderIngestReprocessProcessed"];
	        this.folderIngestPollMs = source["folderIngestPollMs"];
	        this.folderIngestStableMs = source["folderIngestStableMs"];
	        this.canaryEnabled = source["canaryEnabled"];
	        this.canaryIntervalSec = source["canaryIntervalSec"];
	        this.canaryTalkgroup = source["canaryTalkgroup"];
	        this.canaryTalkgroupLabel = source["canaryTalkgroupLabel"];
	        this.configPath = source["configPath"];
	    }
	}

}

