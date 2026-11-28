FROM ghcr.io/psf/httpbin:0.10.4

# HttpBin 0.10.4 allows greenlet 3 on Python >=3.12. Debian trixie ships Python 3.13.
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends python3.13-venv \
    && rm -rf /var/lib/apt/lists/*

USER httpbin
# Recreate the venv at the path used by the upstream launcher, retaining the other package versions.
RUN bin/python -m pip list --format=freeze --exclude pip --exclude gevent --exclude greenlet > /tmp/httpbin-packages.txt \
    && cp -p bin/httpbin.bash /tmp/httpbin.bash \
    && /usr/bin/python3.13 -m venv --clear /opt/httpbin \
    && mv /tmp/httpbin.bash bin/httpbin.bash \
    && bin/python -m pip install --no-cache-dir -r /tmp/httpbin-packages.txt \
        'httpbin[mainapp]' 'gunicorn[gevent]' 'gevent==26.9.0' 'greenlet==3.5.6' \
    && bin/python -m pip check \
    && rm /tmp/httpbin-packages.txt
ENV PATH="/opt/httpbin/bin:${PATH}"
