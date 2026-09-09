import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {visiblePoRepSectors} from '../static/pages/pipeline_porep/visibility.mjs';

test('optional PoRep filter preserves failures, completed SDR and owned SDR', () => {
    const rows = [
        {SectorNumber: 1, SDROwned: false, AfterSDR: false, Failed: false},
        {SectorNumber: 2, SDROwned: true},
        {SectorNumber: 3, SDROwned: false, AfterSDR: true},
        {SectorNumber: 4, SDROwned: false, Failed: true},
        {SectorNumber: 5}, // Legacy payload: do not pretend unknown is unowned.
        {SectorNumber: 6, SDROwned: null},
    ];
    const original = structuredClone(rows);
    assert.deepEqual(visiblePoRepSectors(rows, true).map(r => r.SectorNumber), [2, 3, 4, 5, 6]);
    assert.equal(visiblePoRepSectors(rows, false), rows);
    assert.deepEqual(rows, original);
});

test('actual page uses the filter without hiding RPC rows or falsifying totals', () => {
    const page = readFileSync(new URL('../static/pages/pipeline_porep/pipeline-porep-sectors.mjs', import.meta.url), 'utf8');
    assert.match(page, /visiblePoRepSectors\(this\.data, this\.hidePendingSDR\)/);
    assert.match(page, /visibleSectors\.map\(/);
    assert.match(page, /of \$\{this\.data\.length\}/);
    assert.match(page, /this\.hidePendingSDR = false;/);
    assert.match(page, /attribute: 'hide-pending-sdr'/);
    const api = readFileSync(new URL('../api/webrpcporep/pipeline_porep.go', import.meta.url), 'utf8');
    assert.match(api, /ht_sdr_owner\.id = sp\.task_id_sdr AND ht_sdr_owner\.owner_id > 0\) AS sdr_owned/);
    assert.doesNotMatch(api, /WHERE sp\.failed\s+OR sp\.after_sdr/);
});

test('empty and fully hidden sets remain reversible without altering source rows', () => {
    assert.deepEqual(visiblePoRepSectors([], true), []);
    const rows = [{SectorNumber: 7, SDROwned: false}];
    assert.deepEqual(visiblePoRepSectors(rows, true), []);
    assert.equal(visiblePoRepSectors(rows, false), rows);
    assert.equal(rows.length, 1);
});
