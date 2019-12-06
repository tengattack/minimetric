NAME=minimetric
VERSION=0.0.3
REGISTRY_PREFIX=$(if $(REGISTRY),$(addsuffix /, $(REGISTRY)))

build:
	docker build --build-arg version=${VERSION} \
		--build-arg proxy=${BUILD_HTTP_PROXY} \
		-t ${NAME}:${VERSION} .

publish:
	docker tag ${NAME}:${VERSION} ${REGISTRY_PREFIX}${NAME}:${VERSION}
	docker push ${REGISTRY_PREFIX}${NAME}:${VERSION}

version:
	@echo ${VERSION}
