
**Why the root and not the whole tree.** Resolving the whole tree eagerly to make deep pointers work
would change what the generator resolves and when, on the hot path, for every request — a much larger
change than this slice, and one the recursion budget (`openapi.DefaultRefBudget`) exists to bound.

**Why on `Request` and not on `ResponseVariant`.** `ResponseVariant` is produced by `internal/specs`
from the SPEC (`internal/gen/gen.go:185-198#ResponseVariant`); a patch is produced by the OPERATOR.

**Known consequence, ACCEPTED as a product decision.** A patch is per-variant, while the
list-row/detail-card identity contract spans two variants: patching `GET /users/{id}`'s 200 and not
`GET /users`'s 200 makes the detail card carry a field the list row lacks. Two rounds went round on
whether this is "deferred" or "decided", and the ledger and the prose disagreed
(`rounds/r3/findings.md` row 17). It is DECIDED, here: **the patch is per-variant and this slice adds
no cross-variant rule**, because the alternative — refusing a patch on either half of a list/detail
pair, or silently applying it to both — is a bigger surprise than an operator seeing exactly the
variant they edited. What is deferred is only the UI affordance that would show them the pair, and
that belongs to P2f's editor (R8).

**What it forbids.** Mutating a resolved node in place. Applying a patch inside
`internal/openapi`, or inside `Body`. Leaving either whole-body example gate on recipes alone. A
schema-dependent refusal at ingress. Any per-request patch work.

**What would falsify it.** A patch whose intended effect requires being applied before `$ref`
resolution.

### D4. A patch on a custom endpoint is inert, and the contract says so

**Decision.** `serveCustom` serves a literal body and never generates, so a `schemaPatch` on a
custom endpoint's variant has nothing to apply to. **No code change is made for custom endpoints at
all**: no writer in the tree can put a patch there (F25), so there is nothing to gate and nothing to
refuse. What changes is one sentence of the contract: `Variant.schemaPatch`'s description says both
halves — applied for spec operations, inert for custom endpoints — because the component is shared.

**Why.** `#/components/schemas/Variant` is one shared component referenced by
