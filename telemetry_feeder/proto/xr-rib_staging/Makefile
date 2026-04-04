.PHONY: split generate-go generate-go-split

split:
	python3 ./split_xr_proto.py xr-rib.proto --outdir split

generate-go:
	@echo "xr-rib.proto is a concatenated bundle; run 'make generate-go-split' instead."

generate-go-split: split
	find split -name '*.proto' -print0 | xargs -0 protoc --proto_path=split --go_out=paths=source_relative:split
