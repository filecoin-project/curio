class JsonRpcClient {
    static instance = null;

    static async getInstance() {
        if (!JsonRpcClient.instance) {
            JsonRpcClient.instance = (async () => {
                const client = new JsonRpcClient('/api/webrpc/v0');
                await client.connect();
                return client;
            })();
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

        // Reconnection state
        this._connectPromise = null;
        this._reconnectTimer = null;
        this._shouldReconnect = true;
    }

    async connect() {
        if (this._connectPromise) {
            return this._connectPromise;
        }

        this._shouldReconnect = true;

        this._connectPromise = new Promise((resolve) => {
            const attempt = () => {
                this.ws = new WebSocket(this.url);

                this.ws.onopen = () => {
                    console.log("Connected to the server");
                    // Reset backoff on successful connect
                    this._clearReconnectTimer();
                    // Resolve initial connect promise (subsequent reconnects are transparent)
                    if (this._connectPromise) {
                        // Resolve only once
                        resolve();
                        this._connectPromise = null;
                    }
                };

                this.ws.onclose = () => {
                    console.log("Connection closed, attempting to reconnect...");
                    // Reject all in-flight RPC calls
                    this._rejectAllPending(new Error('WebSocket disconnected'));
                    if (this._shouldReconnect) {
                        this._scheduleReconnect(attempt);
                    }
                };

                this.ws.onerror = (error) => {
                    console.error("WebSocket error:", error);
                    // Force close to unify handling in onclose
                    try { this.ws.close(); } catch (_) {}
                };

                this.ws.onmessage = (message) => {
                    this.handleMessage(message);
                };
            };

            attempt();
        });

        return this._connectPromise;
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

    _scheduleReconnect(attempt) {
        if (this._reconnectTimer) {
            return;
        }
        this._reconnectTimer = setTimeout(() => {
            this._reconnectTimer = null;
            attempt();
        }, 1000);
    }

    _clearReconnectTimer() {
        if (this._reconnectTimer) {
            clearTimeout(this._reconnectTimer);
            this._reconnectTimer = null;
        }
    }

    _rejectAllPending(error) {
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
    const client = await JsonRpcClient.getInstance();
    console.log("webrpc backend:", await client.call('Version', []))
}

init();

export default async function(method, params = []) {
    const i = await JsonRpcClient.getInstance();
    return await i.call(method, params);
}
