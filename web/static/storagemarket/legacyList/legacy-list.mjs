import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class DealDetails extends LitElement {
    constructor() {
        super();
        this.loadData();
    }

    async loadData() {
        const params = new URLSearchParams(window.location.search);
        this.data = await RPCCall('LegacyStorageDealList', [params.get('id')]);
        setTimeout(() => this.loadData(), 5000);
        this.requestUpdate();
    }

    // TODO: Fix DATAtable for legacy deals and test with dummy data
    // TODO: Combine lists maybe?

    render() {
        return html`
            <script src="https://cdnjs.cloudflare.com/ajax/libs/axios/0.21.1/axios.min.js"></script>

            <script src="https://cdnjs.cloudflare.com/ajax/libs/jquery/3.7.1/jquery.min.js"></script>

            <link rel="stylesheet" href="https://cdn.datatables.net/2.0.3/css/dataTables.dataTables.min.css" />
            <script src="https://cdn.datatables.net/2.0.2/js/dataTables.min.js"></script>
            <link rel="stylesheet" href="https://cdn.datatables.net/2.0.3/css/dataTables.bootstrap5.min.css" />

            <link rel="stylesheet" href="https://cdn.datatables.net/scroller/2.4.1/css/scroller.dataTables.min.css" />
            <script src="https://cdn.datatables.net/scroller/2.4.1/js/dataTables.scroller.min.js"></script>

            <link rel="stylesheet" href="https://cdn.datatables.net/responsive/3.0.1/css/responsive.dataTables.min.css" />
            <script src="https://cdn.datatables.net/responsive/3.0.1/js/dataTables.responsive.min.js"></script>

            <link rel="stylesheet" href="https://cdn.datatables.net/buttons/3.0.1/css/buttons.dataTables.min.css" />
            <script src="https://cdn.datatables.net/buttons/3.0.1/js/dataTables.buttons.min.js"></script>

            <link rel="stylesheet" href="https://cdn.datatables.net/select/2.0.0/css/select.dataTables.min.css" />
            <script src="https://cdn.datatables.net/select/2.0.0/js/dataTables.select.min.js"></script>
            <style>
                th {
                    vertical-align: top;
                }
            </style>
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table id="dealTable" class="hover">
                <thead>
                <tr>
                    <th></th>
                    <th class="dd">Miner</th>
                    <th>ID</th>
                    <th>Created At</th>
                    <th>Chain ID</th>
                    <th>Sector</th>
                </tr>
                </thead>
                <tbody>
                <tr>
                    <td>Loading...</td>
                </tr>
                </tbody>
            </table>
            <script>
                const tableData = this.data;
                let dt = new DataTable('#sectorTable', {
                    data: tableData,
                    columns: [
                        { title: "SpID", data: 'Miner',  },
                        {
                            title: "ID", data: 'UUID', render: function (data, type, row) {
                                if (type === 'display') {
                                    return \`<a href="/storagemarket/deal/?id=${deal.UUID}">${data}</a>\`;
                                }
                                return data;
                            }
                        },
                        { title: "Created At", data: 'created_at' },
                        { title: "Chain ID", data: 'chain_deal_id' },
                        { title: "Sector", data: 'sector_num' },
                    ],
                    layout: {
                        topStart: 'buttons',
                        bottomStart: 'info',
                    },
                    buttons: [
                        {
                            extend: 'copy',
                            text: '📋'
                        },
                        'csv',
                        {
                            text: 'Refresh',
                            action: function (e, dt, button, config) {
                                document.cookie = "deal_refresh=true; path=/";
                                location.reload();
                            }
                        },
                    ],
                    responsive: true,
                    order: [[1, 'asc'], [3, 'asc']],
                    select: {
                        style: 'multi',
                        selector: 'td:first-child',
                        items: 'row',
                        rows: '%d rows selected',
                        headerCheckbox: true,
                    },
                    scrollY: window.innerHeight - 250,
                    deferRender: true,
                    scroller: true,
                    initComplete: function () {
                        // all cols with class 'dd' will have a dropdown filter
                        // Add dropdown filters to columns with class 'dd'
                        $('.dd').each(function () {
                            var column = dt.column($(this).index());
                            var select = $('<br><select><option value="">All</option></select>')
                                .appendTo($(this))
                                .on('change', function () {
                                    var val = $.fn.dataTable.util.escapeRegex($(this).val());
                                    column
                                        .search(val ? '^' + val + '$' : '', true, false)
                                        .draw();
                                });
        
                            column
                                .data()
                                .sort()
                                .unique()
                                .each(function (d, j) {
                                    select.append('<option value="' + d + '">' + d + '</option>');
                                });
                        });
                    }
                });
            </script>
        `;

    }
}
customElements.define('deal-details', DealDetails);
