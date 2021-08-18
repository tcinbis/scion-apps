FROM ubuntu:bionic

ARG USER_ID
ARG GROUP_ID

RUN apt-get -qq update && apt-get -qq upgrade -y && \
    DEBIAN_FRONTEND=noninteractive apt-get -qq install -y sudo \
                                                          git \
                                                          build-essential \
                                                          make \
                                                          apt-utils \
                                                          software-properties-common \
                                                          apt-transport-https \
                                                          curl \
                                                          gnupg > /dev/null && rm -rf /var/lib/apt/lists/*
RUN add-apt-repository ppa:longsleep/golang-backports
RUN apt-get -qq update > /dev/null && \
    DEBIAN_FRONTEND=noninteractive apt-get -qq install -y golang > /dev/null && \
    rm -rf /var/lib/apt/lists/*

RUN echo '%sudo ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers
RUN groupadd -g $GROUP_ID scion && \
    groupadd docker && \
    useradd -u $USER_ID -g $GROUP_ID -ms /bin/bash scion && \
    usermod -a -G sudo scion && \
    usermod -a -G docker scion

USER scion
RUN mkdir -p /home/scion/
WORKDIR /home/scion/