# Curio build entrypoint.
#
# The project Makefile is intentionally split into focused include files under
# scripts/makefiles/ to keep behavior coherent and maintainable.
#
# See documentation/en/developer/make-variables.md for variable/override guidance.

include scripts/makefiles/00-vars.mk
include scripts/makefiles/05-help.mk
include scripts/makefiles/10-deps.mk
include scripts/makefiles/20-test.mk
include scripts/makefiles/30-build.mk
include scripts/makefiles/40-gen.mk
include scripts/makefiles/50-docker.mk
include scripts/makefiles/60-abi.mk
