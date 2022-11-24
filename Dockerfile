# Copyright the Velero contributors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM --platform=$BUILDPLATFORM golang:1.18-bullseye AS build

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

ENV GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT}

COPY . /go/src/velero-plugin-for-microsoft-azure
WORKDIR /go/src/velero-plugin-for-microsoft-azure
RUN export GOARM=$( echo "${GOARM}" | cut -c2-) && \
    CGO_ENABLED=0 go build -v -o /go/bin/velero-plugin-for-microsoft-azure ./velero-plugin-for-microsoft-azure

FROM busybox:1.34.1 AS busybox

FROM scratch
COPY --from=build /go/bin/velero-plugin-for-microsoft-azure /plugins/
COPY --from=busybox /bin/cp /bin/cp
USER 65532:65532
ENTRYPOINT ["cp", "/plugins/velero-plugin-for-microsoft-azure", "/target/."]
