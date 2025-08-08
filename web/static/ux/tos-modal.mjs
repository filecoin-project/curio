import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class TosModal extends LitElement {
  static properties = {
    show: { type: Boolean },
    type: { type: String }, // 'client' or 'provider'
    tosContent: { type: Object },
    loading: { type: Boolean },
  };

  constructor() {
    super();
    this.show = false;
    this.type = 'client';
    this.tosContent = null;
    this.loading = false;
  }

  createRenderRoot() {
    // Use light DOM so Bootstrap + main.css apply
    return this;
  }

  /**
   * Load TOS content from the server
   */
  async loadTosContent() {
    if (!this.tosContent) {
      this.loading = true;
      this.requestUpdate();
      
      try {
        this.tosContent = await RPCCall('PSGetTos', []);
      } catch (err) {
        console.error('Error loading TOS content:', err);
        this.tosContent = { 
          Client: 'Error loading Terms of Service',
          Provider: 'Error loading Terms of Service'
        };
      }
      
      this.loading = false;
      this.requestUpdate();
    }
    return this.tosContent;
  }

  /**
   * Show the modal and load TOS content
   */
  async showModal(type = 'client') {
    this.type = type;
    await this.loadTosContent();
    this.show = true;
    this.requestUpdate();
  }

  /**
   * Handle TOS acceptance
   */
  handleAccept() {
    const storageKey = `curio-proofshare-${this.type}-tos-accepted`;
    localStorage.setItem(storageKey, 'true');
    this.show = false;
    this.requestUpdate();
    
    // Dispatch custom event for parent to handle
    this.dispatchEvent(new CustomEvent('tos-accepted', {
      detail: { type: this.type },
      bubbles: true
    }));
  }

  /**
   * Handle TOS rejection
   */
  handleReject() {
    this.show = false;
    this.requestUpdate();
    
    // Dispatch custom event for parent to handle
    this.dispatchEvent(new CustomEvent('tos-rejected', {
      detail: { type: this.type },
      bubbles: true
    }));
  }

  /**
   * Check if TOS has been accepted from localStorage
   */
  static checkTosAcceptance(type) {
    return localStorage.getItem(`curio-proofshare-${type}-tos-accepted`) === 'true';
  }

  render() {
    if (!this.show) return html``;

    const title = this.type === 'client' ? 'Client Terms of Service' : 'Provider Terms of Service';
    const content = this.type === 'client' ? 
      (this.tosContent?.client || this.tosContent?.Client) : 
      (this.tosContent?.provider || this.tosContent?.Provider);

    return html`
      <div class="modal d-block" style="background-color: rgba(0,0,0,0.5);">
        <div class="modal-dialog modal-lg">
          <div class="modal-content bg-dark text-light">
            <div class="modal-header">
              <h5 class="modal-title">${title}</h5>
            </div>
            <div class="modal-body" style="max-height: 400px; overflow-y: auto;">
              <p>By enabling ProofShare ${this.type === 'client' ? 'Client' : 'Provider'}, you agree to the following terms:</p>
              <div style="white-space: pre-wrap; font-family: monospace; font-size: 0.9em; background-color: #2d2d2d; padding: 15px; border-radius: 5px;">
                ${this.loading ? 'Loading terms...' : (content || 'Terms not available')}
              </div>
            </div>
            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click=${this.handleReject}>
                Decline
              </button>
              <button type="button" class="btn btn-primary" @click=${this.handleAccept} ?disabled=${this.loading}>
                Accept
              </button>
            </div>
          </div>
        </div>
      </div>
    `;
  }
}

customElements.define('tos-modal', TosModal); 