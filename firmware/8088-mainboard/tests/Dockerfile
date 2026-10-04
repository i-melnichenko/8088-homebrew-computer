FROM python:3.12-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends make nasm \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /work
COPY tests/requirements.txt tests/requirements.txt
RUN pip install --no-cache-dir -r tests/requirements.txt
COPY . .

# Rebuild inside Linux rather than trusting a host-generated ROM image.
ARG BIOS_VERSION=dev
ENV BIOS_VERSION=${BIOS_VERSION}
RUN make -B bios citylights terminal BIOS_VERSION="${BIOS_VERSION}"
CMD ["python", "tests/test_bios.py"]
