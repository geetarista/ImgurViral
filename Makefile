ERROR_COLOR=\033[31;01m
NO_COLOR=\033[0m
OK_COLOR=\033[32;01m
WARN_COLOR=\033[33;01m
DEPS = $(go list -f '{{range .TestImports}}{{.}} {{end}}' ./...)

deploy:
	@echo "$(OK_COLOR)==> Deploying...$(NO_COLOR)"
	@appcfg.py --oauth2 update .

deps:
	@echo "$(OK_COLOR)==> Installing dependencies...$(NO_COLOR)"
	@go get -d -v ./...
	@echo $(DEPS) | xargs -n1 go get -d

serve:
	@echo "$(OK_COLOR)==> Serving at http://localhost:8080$(NO_COLOR)"
	@echo "$(OK_COLOR)  dashboard at http://localhost:8000$(NO_COLOR)"
	@echo "$(OK_COLOR)        api at http://localhost:50509$(NO_COLOR)"
	@goapp serve

test:
	@echo "$(OK_COLOR)==> Running tests...$(NO_COLOR)"
	@-goapp test github.com/geetarista/ImgurViral -v
	@if [ -f ImgurViral.test ]; then rm ImgurViral.test; fi

.PHONY: deploy, deps, serve, test
