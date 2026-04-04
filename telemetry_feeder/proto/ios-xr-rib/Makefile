.PHONY: rewrite-go-package generate-go

PROTO_ROOT := .
MODULE_IMPORT_ROOT := github.com/sbezverk/tools/telemetry_feeder/proto/ios-xr-rib

rewrite-go-package:
	@find $(PROTO_ROOT) -name 'schema.proto' | while read -r proto; do \
		dir=$$(dirname "$$proto"); \
		pkg=$$(basename "$$dir"); \
		perl -0pi -e 's#option go_package = ".*?";#option go_package = "$(MODULE_IMPORT_ROOT)/'"$$dir"';'"$$pkg"'";#s' "$$proto"; \
	done

generate-go: rewrite-go-package
	find $(PROTO_ROOT) -name 'schema.proto' -print0 | xargs -0 protoc --proto_path=$(PROTO_ROOT) --go_out=paths=source_relative:$(PROTO_ROOT)
