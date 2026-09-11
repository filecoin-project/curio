// Sparse layer safety checks shared by the real editor and database-free tests.
// Do not replace an unknown or unrepresentable value with a default.
export function resolveSchema(root, schema) {
    const seen = new Set();
    while (schema?.$ref) {
        const ref = schema.$ref;
        if (!ref.startsWith('#/') || seen.has(ref)) throw new Error(`Unsupported schema reference: ${ref}`);
        seen.add(ref);
        schema = ref.slice(2).split('/').reduce((node, key) => node?.[key.replace(/~1/g, '/').replace(/~0/g, '~')], root);
        if (!schema) throw new Error(`Unresolved schema reference: ${ref}`);
    }
    return schema;
}

export function validateLayerShape(value, root, schema = root, path = 'Configuration') {
    schema = resolveSchema(root, schema);
    if (!schema || schema === false) throw new Error(`Unsupported configuration field: ${path}`);
    if (value === null) throw new Error(`NULL is not a configuration value: ${path}`);
    const type = Array.isArray(value) ? 'array' : typeof value;
    if (schema.type === 'integer') {
        if (!Number.isSafeInteger(value)) throw new Error(`Expected a safely representable integer: ${path}`);
    } else if (schema.type !== type) {
        throw new Error(`Configuration type mismatch: ${path} (expected ${schema.type}, received ${type})`);
    }
    if (type === 'object') {
        for (const [key, child] of Object.entries(value)) {
            const childSchema = Object.hasOwn(schema.properties || {}, key) ? schema.properties[key] : schema.additionalProperties;
            if (!childSchema || childSchema === false) throw new Error(`Unsupported configuration field: ${path}.${key}; no changes saved`);
            validateLayerShape(child, root, childSchema, `${path}.${key}`);
        }
    } else if (type === 'array') {
        value.forEach((child, index) => validateLayerShape(child, root, schema.items, `${path}[${index}]`));
    }
}

// Run immediately after JSON Editor loads a layer, before any user edits. An
// editor that omits/coerces an existing value must not offer a destructive save.
export function assertLayerLoaded(original, represented, path = 'Configuration') {
    if (original !== null && typeof original === 'object') {
        if (!represented || Array.isArray(original) !== Array.isArray(represented)) throw new Error(`Editor lost ${path}`);
        if (Array.isArray(original) && original.length !== represented.length) throw new Error(`Editor changed ${path}`);
        for (const [key, value] of Object.entries(original)) {
            if (!Object.hasOwn(represented, key)) throw new Error(`Editor lost ${path}.${key}; no changes saved`);
            assertLayerLoaded(value, represented[key], `${path}.${key}`);
        }
    } else if (original !== represented) {
        throw new Error(`Editor changed ${path}; no changes saved`);
    }
}
