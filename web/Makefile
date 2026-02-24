
node_modules: package.json
	npm install

dist: node_modules src
	npm run build

.PHONY: serve
serve: node_modules
	npm run dev

.PHONY: clean
clean:
	rm -rf dist
