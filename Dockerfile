FROM eclipse-temurin:8-jre

ENV EMBULK_VERSION 0.11.4
ENV JRUBY_VERSION 9.4.8.0

WORKDIR /app

# jruby
RUN curl -o /app/jruby-complete-${JRUBY_VERSION}.jar --create-dirs -L "https://repo1.maven.org/maven2/org/jruby/jruby-complete/${JRUBY_VERSION}/jruby-complete-${JRUBY_VERSION}.jar"


# embulk
RUN curl -o /app/embulk-${EMBULK_VERSION}.jar --create-dirs -L "https://github.com/embulk/embulk/releases/download/v${EMBULK_VERSION}/embulk-${EMBULK_VERSION}.jar" \
  && ln -s /app/embulk-${EMBULK_VERSION}.jar /app/embulk.jar

RUN mkdir -p logs && groupadd -g 1001 embulk && useradd -m -g embulk -u 1001 embulk

USER 1001

ADD --chown=1001:1001 embulk.properties /home/embulk/.embulk/embulk.properties
RUN mkdir -p logs \
  && mkdir -p /home/embulk/.local/share/gem/jruby/3.1.0/cache/ \
  && java -jar /app/embulk.jar gem install embulk -v ${EMBULK_VERSION} \
  && java -jar /app/embulk.jar gem install bundler -v 1.16.0 \
  && java -jar /app/embulk.jar gem install liquid -v 4.0.0 \
  && java -jar /app/embulk.jar gem install embulk-input-mysql -v 0.13.2 \
  && java -jar /app/embulk.jar gem install embulk-output-mysql -v 0.10.5 \
  && java -jar /app/embulk.jar gem install msgpack -v 1.2.4

ADD  --chown=1001:1001 Gemfile Gemfile.lock .bundle /app/
RUN java -jar /app/embulk.jar bundle 
RUN java -jar /app/embulk.jar install org.embulk:embulk-input-mysql:0.13.2

ENTRYPOINT ["java", "-jar", "/app/embulk.jar", "run"]