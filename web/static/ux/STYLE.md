# Curio Admin Style Language

Normative reference for the Curio web UI. Synthesizes ideas from GitHub Primer (token architecture, accessibility), Linear (restraint, density rhythm), and Datadog DRUIDS (operational data patterns).

## Principles

1. **Chrome recedes, data leads** — navigation and framing use lower contrast than tables, metrics, and actionable content.
2. **One accent as punctuation** — violet/blue accent reserved for links, focus rings, and active navigation. No decorative color.
3. **Hierarchy by weight, not color** — headings use font weight and size; body text stays neutral.
4. **Borders over shadows** — structure from 1px hairlines and surface layering. Shadows only on overlays (drawers, modals).
5. **Density with rhythm** — table/list/form rows target 32px height; spacing on a 4px scale.
6. **Status color is semantic** — success/warning/danger/info always paired with text or icons; never color-only.
7. **AA contrast always** — 4.5:1 normal text, 3:1 large text and UI controls.

## Token Architecture

Three tiers (Primer-inspired):

| Tier | Usage | Example |
|------|-------|---------|
| Base | Raw values in `main.css` only; never referenced in components | `#0d1117` |
| Functional | Public API — all components use these | `--color-bg-canvas` |
| Component | Defined in `main.css` as utility classes | `.info-block`, `.badge-danger` |

### Functional tokens

| Token | Value | Role |
|-------|-------|------|
| `--color-bg-canvas` | `#0d1117` | Page background |
| `--color-bg-subtle` | `#161b22` | Cards, panels |
| `--color-bg-elevated` | `#21262d` | Inputs, hover, active nav |
| `--color-border-default` | `#30363d` | Hairline borders |
| `--color-border-muted` | `#21262d` | Subtle dividers |
| `--color-text-primary` | `#e6edf3` | Body text |
| `--color-text-secondary` | `#8b949e` | Labels, metadata |
| `--color-text-muted` | `#656d76` | Placeholder, disabled |
| `--color-accent-fg` | `#4493f8` | Links |
| `--color-accent-emphasis` | `#5e6ad2` | Focus, active nav accent |
| `--color-success-fg` | `#3fb950` | Success status |
| `--color-warning-fg` | `#d29922` | Warning status |
| `--color-danger-fg` | `#f85149` | Error status |
| `--color-info-fg` | `#58a6ff` | Info status |

Muted status backgrounds use 15% alpha of the foreground token.

Legacy aliases (`--color-bg`, `--color-fg`, `--color-primary-main`, etc.) map to functional tokens for backward compatibility.

## Typography

- **UI:** Inter 400 (body), 500 (labels), 600 (headings)
- **Data:** `ui-monospace` for PieceCIDs, addresses, git hashes, epochs, SQL

| Role | Size | Weight |
|------|------|--------|
| Page title | 20px | 600 |
| Section title | 16px | 600 |
| Body | 14px | 400 |
| Label / table header | 12px | 500, uppercase optional |
| Caption | 12px | 400 |
| Mono data | 13px | 400 |

## Spacing & Radius

- Spacing scale: 4, 8, 12, 16, 20, 24px
- Card padding: 16–20px
- Card radius: 8px; controls: 6px
- Sidebar width: 240px

## Component Rules

### Card (`.info-block`)
- Background: `--color-bg-subtle`
- Border: 1px `--color-border-default`
- Radius: 8px; padding: 20px
- Section title: bottom border, 16px/600

### Table (`.table-dark`)
- Row height ~32px; header 12px uppercase
- Zebra: 1.5% white overlay; hover: `--color-table-hover`
- Mono font on hash/CID columns via `.mono`

### Nav item (sidebar)
- Height 32px; radius 6px
- Default: secondary text; hover: elevated bg
- Active: elevated bg + 2px left accent bar (`--color-accent-emphasis`)

### Button
- Primary: accent emphasis bg, white text
- Secondary: elevated bg, default border
- Disabled: dedicated muted tokens, not opacity

### Badge
- Small caps label + muted background matching semantic fg

## Do / Don't

**Do**
- Use functional tokens in all new code
- Pair status colors with text labels
- Keep sidebar quieter than content area
- Link entity IDs (PieceCID, sector, task name)

**Don't**
- Hardcode hex in components
- Use shadows for layout (only overlays)
- Use color as the only status indicator
- Add new colors without a token
- Use monospace for general UI text
