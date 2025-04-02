FROM apache/cloudstack-simulator:4.20.0.0


WORKDIR /root

RUN mvn -pl client package -DskipTests -Dsimulator

# RUN sed -i 's|mvn -pl client jetty:run|java -jar /root/client/target/cloud-client-ui-4.20.0.0.jar|g' /etc/supervisor/conf.d/supervisord.conf


