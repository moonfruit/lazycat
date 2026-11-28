FROM ghcr.io/psf/httpbin:0.10.4

# Gunicorn's gevent worker requires a newer greenlet than HttpBin supports on Python 3.10.
RUN grep -Fq -- '-k gevent' /opt/httpbin/bin/httpbin.bash \
    && sed -i 's/-k gevent/-k gthread --threads 4/' /opt/httpbin/bin/httpbin.bash \
    && /opt/httpbin/bin/python -m pip check
