import { html, LitElement } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class MarketBalance extends LitElement {
    static properties = {
        balanceData: { type: Array },
        selectedMiner: { type: String },
        amount: { type: String },
        wallet: { type: String },
    };

    constructor() {
        super();
        this.balanceData = [];
        this.selectedMiner = '';
        this.amount = '';
        this.wallet = '';
        this.loadData();
    }

    async loadData() {
        try {
            this.balanceData = await RPCCall('MarketBalance');
            setTimeout(() => this.loadData(), 10000);
            this.requestUpdate();
        } catch (error) {
            console.error('Failed to load market balances:', error);
        }
    }

    handleMinerChange(event) {
        this.selectedMiner = event.target.value;
    }

    handleAmountChange(event) {
        this.amount = event.target.value;
    }

    handleWalletChange(event) {
        this.wallet = event.target.value;
    }

    async handleSubmit(event) {
        event.preventDefault();
        try {
            const result = await RPCCall('MoveBalanceToEscrow', [
                this.selectedMiner,
                this.amount,
                this.wallet,
            ]);
            alert('Funds moved to escrow successfully with message: ' + result);
            this.loadData(); // Refresh data to reflect changes
        } catch (error) {
            alert('Error moving funds to escrow: ' + error.message);
        }
    }

    render() {
        return html`
      <!-- Include Bootstrap CSS -->
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
        integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
        crossorigin="anonymous"
      />
      <link
        rel="stylesheet"
        href="/ux/main.css"
        onload="document.body.style.visibility = 'initial'"
      />

      <div class="container mt-4">
        <h2>Market Balances</h2>
        <table class="table table-dark table-striped table-sm">
          <thead>
            <tr>
              <th>Miner</th>
              <th>Market Balance</th>
              <th>Deal Publish Address</th>
              <th>Balance</th>
            </tr>
          </thead>
          <tbody>
            ${this.balanceData.map(
            (mb) => html`
                ${mb.balances && mb.balances.length > 0
                ? mb.balances.map(
                    (bal, index) => html`
                        <tr>
                          ${index === 0
                        ? html`
                                <td rowspan="${mb.balances.length}">
                                  ${mb.miner}
                                </td>
                                <td rowspan="${mb.balances.length}">
                                  ${mb.market_balance}
                                </td>
                              `
                        : ''}
                          <td>${bal.address}</td>
                          <td>${bal.balance}</td>
                        </tr>
                      `
                )
                : html`
                      <tr>
                        <td>${mb.miner}</td>
                        <td>${mb.market_balance}</td>
                        <td colspan="2">No addresses</td>
                      </tr>
                    `}
              `
        )}
          </tbody>
        </table>

        <!-- Form to Move Balance to Escrow -->
        <h3>Move Balance to Escrow</h3>
        <form @submit="${this.handleSubmit}" style="padding-bottom: 20px">
          <div class="mb-3">
            <label for="minerSelect" class="form-label">Select Miner</label>
            <select
              id="minerSelect"
              class="form-select"
              .value="${this.selectedMiner}"
              @change="${this.handleMinerChange}"
              required
            >
              <option value="" disabled selected>Select a miner</option>
              ${this.balanceData.map(
            (mb) => html`
                  <option value="${mb.miner}">${mb.miner}</option>
                `
        )}
            </select>
          </div>
          <div class="mb-3">
            <label for="amountInput" class="form-label">Amount</label>
            <input
              type="text"
              id="amountInput"
              class="form-control"
              .value="${this.amount}"
              @input="${this.handleAmountChange}"
              required
            />
          </div>
          <div class="mb-3">
            <label for="walletInput" class="form-label">Wallet</label>
            <input
              type="text"
              id="walletInput"
              class="form-control"
              .value="${this.wallet}"
              @input="${this.handleWalletChange}"
              required
            />
          </div>
          <button type="submit" class="btn btn-primary">
            Move Funds to Escrow
          </button>
        </form>
      </div>
    `;
    }
}

customElements.define('market-balance', MarketBalance);
