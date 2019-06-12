FROM httpd:2.4
COPY ./index.html /usr/local/apache2/htdocs/
RUN mkdir /usr/local/apache2/htdocs/auth-digest/
COPY ./auth-digest/ /usr/local/apache2/htdocs/auth-digest/
COPY ./httpd.conf /usr/local/apache2/conf/httpd.conf
RUN mkdir /var/log/apache2
