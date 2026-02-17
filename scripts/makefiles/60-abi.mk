# ABI helper rule.

# Download ABIs directly from PDP release:
# curl -L https://github.com/FilOzone/pdp/releases/download/vX.Y.Z/PDPVerifier.abi.json \
#      -o pdp/contract/PDPVerifier.abi
# curl -L https://github.com/FilOzone/pdp/releases/download/vX.Y.Z/IPDPProvingSchedule.abi.json \
#      -o pdp/contract/IPDPProvingSchedule.abi

%.go: %.abi
	abigen --abi $^ --type $(notdir $*) --pkg $(notdir $(patsubst %/,%,$(dir $@))) --out $@
