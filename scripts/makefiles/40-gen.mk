# Code/documentation generation targets.

cfgdoc-gen:
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./deps/config/cfgdocgen > ./deps/config/doc_gen.go

fix-imports:
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./scripts/fiximports

docsgen: docsgen-md docsgen-openrpc
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen

docsgen-md: docsgen-md-curio
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen-md

api-gen:
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./api/gen/api/proxygen.go
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: api-gen

docsgen-md-curio: docsgen-md-bin
	echo '---' > documentation/en/developer/api.md
	echo 'description: Curio API references' >> documentation/en/developer/api.md
	echo '---' >> documentation/en/developer/api.md
	echo '' >> documentation/en/developer/api.md
	echo '# API' >> documentation/en/developer/api.md
	echo '' >> documentation/en/developer/api.md
	./docgen-md "api/api_curio.go" "Curio" "api" "./api" >> documentation/en/developer/api.md
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen-md-curio

docsgen-md-bin: api-gen
	$(GOCC) build $(GOFLAGS) -tags="$(CURIO_TAGS)" -o docgen-md ./scripts/docgen/cmd
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen-md-bin

docsgen-openrpc: docsgen-openrpc-curio
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen-openrpc

docsgen-openrpc-bin: api-gen
	$(GOCC) build $(GOFLAGS) -tags="$(CURIO_TAGS)" -o docgen-openrpc ./api/docgen-openrpc/cmd

docsgen-openrpc-curio: docsgen-openrpc-bin
	./docgen-openrpc "api/api_curio.go" "Curio" "api" "./api" > build/openrpc/curio.json

docsgen-cli: curio sptool
	python3 ./scripts/generate-cli.py
	echo '---' > documentation/en/configuration/default-curio-configuration.md
	echo 'description: The default curio configuration' >> documentation/en/configuration/default-curio-configuration.md
	echo '---' >> documentation/en/configuration/default-curio-configuration.md
	echo '' >> documentation/en/configuration/default-curio-configuration.md
	echo '# Default Curio Configuration' >> documentation/en/configuration/default-curio-configuration.md
	echo '' >> documentation/en/configuration/default-curio-configuration.md
	echo '```toml' >> documentation/en/configuration/default-curio-configuration.md
	LANG=en-US ./curio config default >> documentation/en/configuration/default-curio-configuration.md
	echo '```' >> documentation/en/configuration/default-curio-configuration.md
.PHONY: docsgen-cli

docsgen-metrics:
	$(GOCC) run $(GOFLAGS) ./scripts/metricsdocgen > documentation/en/configuration/metrics-reference.md
.PHONY: docsgen-metrics

translation-gen:
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./scripts/translationcheck
.PHONY: translation-gen

go-generate:
	@bash -lc 'set -euo pipefail; \
	  CGO_ALLOW="$(subst ",,$(CGO_LDFLAGS_ALLOW))"; \
	  GO_FLAGS="$(GOFLAGS) -tags=$(CURIO_TAGS_CSV)"; \
	  for p in $$(go list ./...); do \
	    tf="$$(mktemp -t go-gen-time.XXXXXX)"; \
	    cmd=(env CGO_LDFLAGS_ALLOW="$$CGO_ALLOW" GOFLAGS="$$GO_FLAGS" $(GOCC) generate "$$p"); \
	    printf "CMD: "; printf "%q " "$${cmd[@]}"; echo ""; \
	    if /usr/bin/time -p -o "$$tf" "$${cmd[@]}"; then \
	      : ; \
	    else \
	      rc="$$?"; \
	      echo "FAILED: $$p (exit $$rc)"; \
	      grep "^real " "$$tf" || true; \
	      rm -f "$$tf" || true; \
	      exit "$$rc"; \
	    fi; \
	    echo "### timing for $$p ###"; \
	    grep "^real " "$$tf"; \
	    rm -f "$$tf"; \
	  done'
.PHONY: go-generate

gen: gensimple
.PHONY: gen

marketgen:
	swag init -dir market/mk20/http -g http.go  -o market/mk20/http --parseDependencyLevel 3 --parseDependency
.PHONY: marketgen

gen-deps: $(BUILD_DEPS)
	@echo "Built dependencies with FVM support for testing"
.PHONY: gen-deps

# Run gen steps sequentially in a single shell to avoid Go build cache race conditions.
# The "unlinkat: directory not empty" error occurs when multiple go processes
# contend for the same build cache simultaneously.
# Set GOCACHE_CLEAN=1 to clear the build cache before running (fixes persistent issues).
gensimple: export FFI_USE_OPENCL=1
gensimple:
ifeq ($(GOCACHE_CLEAN),1)
	$(GOCC) clean -cache
endif
	@bash -lc '\
		set -euo pipefail; \
		t() { name="$$1"; shift; \
			start=$$(date +%s); \
			"$$@"; \
			end=$$(date +%s); \
			echo "TIMING $$name: $$((end-start))s"; \
		}; \
		t gen-deps    $(MAKE) gen-deps; \
		t api-gen     $(MAKE) api-gen; \
		t go-generate $(MAKE) go-generate; \
		t translation-gen $(MAKE) translation-gen; \
		t cfgdoc-gen  $(MAKE) cfgdoc-gen; \
		t docsgen $(MAKE) docsgen; \
		t marketgen   $(MAKE) marketgen; \
		t docsgen-cli $(MAKE) docsgen-cli; \
		t docsgen-metrics $(MAKE) docsgen-metrics; \
	'
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./scripts/fiximports
.PHONY: gensimple

fiximports:
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./scripts/fiximports
.PHONY: fiximports
