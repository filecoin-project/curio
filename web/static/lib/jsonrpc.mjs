class JsonRpcClient {
    static instance = null;

    static async getInstance() {
        if (!JsonRpcClient.instance) {
            JsonRpcClient.instance = (async () => {
                const client = new JsonRpcClient('/api/webrpc/v0');
                await client.connect();
                return client;
            })().catch((err) => {
                JsonRpcClient.instance = null;
                throw err;
            });
        }
        return await JsonRpcClient.instance;
    }


    constructor(url) {
        if (JsonRpcClient.instance) {
            throw new Error("Error: Instantiation failed: Use getInstance() instead of new.");
        }
        this.url = url;
        this.requestId = 0;
        this.pendingRequests = new Map();

        this.connectPromise = null;
        this.reconnectTimer = null;
        this.shouldReconnect = true;
    }

    async connect() {
        if (this.connectPromise) {
            return this.connectPromise;
        }

        this.shouldReconnect = true;

        this.connectPromise = new Promise((resolve, reject) => {
            let hasOpened = false;

            const attempt = () => {
                this.ws = new WebSocket(this.url);

                this.ws.onopen = () => {
                    hasOpened = true;
                    console.log("Connected to the server");
                    this.clearReconnectTimer();
                    if (this.connectPromise) {
                        resolve();
                        this.connectPromise = null;
                    }
                };

                this.ws.onclose = () => {
                    console.log("Connection closed, attempting to reconnect...");
                    this.rejectAllPending(new Error('WebSocket disconnected'));
                    if (!hasOpened) {
                        this.shouldReconnect = false;
                        this.clearReconnectTimer();
                        if (this.connectPromise) {
                            reject(new Error('WebSocket initial connection failed'));
                            this.connectPromise = null;
                        }
                        return;
                    }
                    if (this.shouldReconnect) {
                        this.scheduleReconnect(attempt);
                    }
                };

                this.ws.onerror = (error) => {
                    console.error("WebSocket error:", error);
                    try { this.ws.close(); } catch (_) {}
                };

                this.ws.onmessage = (message) => {
                    this.handleMessage(message);
                };
            };

            attempt();
        });

        return this.connectPromise;
    }

    handleMessage(message) {
        const response = JSON.parse(message.data);
        const { id, result, error } = response;

        const resolver = this.pendingRequests.get(id);
        if (resolver) {
            if (error) {
                resolver.reject(error);
            } else {
                resolver.resolve(result);
            }
            this.pendingRequests.delete(id);
        }
    }

    call(method, params = []) {
        const id = ++this.requestId;
        const request = {
            jsonrpc: "2.0",
            method: "CurioWeb." + method,
            params,
            id,
        };

        return new Promise((resolve, reject) => {
            this.pendingRequests.set(id, { resolve, reject });

            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                try {
                    this.ws.send(JSON.stringify(request));
                } catch (e) {
                    this.pendingRequests.delete(id);
                    reject(e);
                }
            } else {
                this.pendingRequests.delete(id);
                reject('WebSocket is not open');
            }
        });
    }

    scheduleReconnect(attempt) {
        if (this.reconnectTimer) {
            return;
        }
        this.reconnectTimer = setTimeout(() => {
            this.reconnectTimer = null;
            attempt();
        }, 1000);
    }

    clearReconnectTimer() {
        if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
            this.reconnectTimer = null;
        }
    }

    rejectAllPending(error) {
        const err = error instanceof Error ? error : new Error(String(error || 'WebSocket disconnected'));
        for (const [id, resolver] of this.pendingRequests.entries()) {
            try {
                resolver.reject(err);
            } catch (_) {}
        }
        this.pendingRequests.clear();
    }
}

async function init() {
    try {
        const client = await JsonRpcClient.getInstance();
        console.log("webrpc backend:", await client.call('Version', []));
    } catch (_) {
        // backend may be unavailable during static-only dev
    }
}

init();

export default async function(method, params = []) {
    const i = await JsonRpcClient.getInstance();
    return await i.call(method, params);
}

// HTTP JSON-RPC (not WebSocket). Use when the server needs request headers
// such as Referer — browsers do not send Referer on WebSocket upgrades.
// Pass AbortSignal via opts.signal to cancel; the server cancels the DB query.
export async function RPCCallHTTP(method, params = [], opts = {}) {
    const res = await fetch('/api/webrpc/v0', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            jsonrpc: '2.0',
            method: 'CurioWeb.' + method,
            params,
            id: 1,
        }),
        signal: opts.signal,
    });

    let body;
    try {
        body = await res.json();
    } catch (e) {
        if (opts.signal?.aborted || e?.name === 'AbortError') {
            throw e;
        }
        throw new Error(`RPC HTTP ${res.status}: invalid JSON response`);
    }

    if (body?.error) {
        const err = body.error;
        throw new Error(err.message || err.Message || String(err));
    }
    if (!res.ok) {
        throw new Error(`RPC HTTP ${res.status}`);
    }
    return body.result;
}
