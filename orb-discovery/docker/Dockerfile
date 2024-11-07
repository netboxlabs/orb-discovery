FROM python:3.12-bullseye

WORKDIR /usr/src/app

COPY orb-discovery/ ./orb-discovery

RUN pip install --no-cache-dir ./orb-discovery/

RUN rm -rf ./orb-discovery

CMD [ "orb-discovery" ]