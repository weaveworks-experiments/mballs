FROM ubuntu
WORKDIR /home
ADD ./mballs /home/
CMD []
ENTRYPOINT ["/home/mballs", "-iface", "ethwe"]
