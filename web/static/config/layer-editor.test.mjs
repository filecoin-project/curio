import test from 'node:test';
import assert from 'node:assert/strict';
import { validateLayerShape, assertLayerLoaded } from './layer-editor.mjs';

const schema = {
    $ref: '#/$defs/Root',
    $defs: {
        Root: { type: 'object', properties: { Subsystems: { $ref: '#/definitions/Subsystems' }, Ingest: { $ref: '#/$defs/Ingest' } } },
        Ingest: { type: 'object', properties: { Batch: { type: 'integer' }, Limit: { type: 'integer' } } },
    },
    definitions: { Subsystems: { type: 'object', properties: {
        StartDelay: { type: 'string' }, Enabled: { type: 'boolean' }, Tasks: { type: 'integer' },
    } } },
};

test('multiple custom overrides, no-edit and one-field edit preserve others', () => {
    for (const [tasks, interval] of [[2, '30s'], [3, '2m']]) {
        const original = { Subsystems: { Tasks: tasks, StartDelay: interval, Enabled: true },
            Ingest: { Batch: 0, Limit: 17 } };
        const loaded = structuredClone(original);
        validateLayerShape(loaded, schema);
        assertLayerLoaded(original, loaded);
        loaded.Subsystems.Tasks++;
        validateLayerShape(loaded, schema);
        assert.equal(loaded.Subsystems.StartDelay, interval);
        assert.deepEqual(loaded.Ingest, original.Ingest);
    }
});

test('missing, coerced and unknown fields stop destructive save', () => {
    const original = { Subsystems: { Enabled: false } };
    assert.throws(() => assertLayerLoaded(original, { Subsystems: {} }), /Editor lost/);
    assert.throws(() => assertLayerLoaded(original, { Subsystems: { Enabled: true } }), /Editor changed/);
    assert.throws(() => validateLayerShape({ Subsystems: { UnrecognizedOption: 7 } }, schema), /Unsupported configuration field/);
    assert.throws(() => validateLayerShape({ Subsystems: { Enabled: {} } }, schema), /type mismatch/);
});

test('invalid references, NULL and unsafe integer precision fail closed', () => {
    assert.throws(() => validateLayerShape({}, { $ref: '#/$defs/Missing' }), /Unresolved/);
    assert.throws(() => validateLayerShape({ Subsystems: null }, schema), /NULL/);
    assert.throws(() => validateLayerShape({ Ingest: { Batch: 2 ** 53 } }, schema), /safely representable/);
});

test('arbitrary nested maps/arrays and empty strings survive without cleanup', () => {
    const s = { type: 'object', additionalProperties: { type: 'array', items: { type: 'string' } } };
    const layer = { ArbitraryKey: ['', 'value'], Empty: [] };
    validateLayerShape(layer, s);
    assertLayerLoaded(layer, structuredClone(layer));
    assert.deepEqual(layer.ArbitraryKey, ['', 'value']);
});
