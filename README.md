# README #

Servicio construido en Go con el framework Gin. Se encarga de la recolección y procesamiento de datos de múltiples OLTS (Optical Line Terminals) de diferentes fabricantes (ZTE, CData, VSOL). Este microservicio está diseñado para operar en un entorno de alta carga, gestionando más de 40 OLTS y 30k+ clientes (onus) de manera eficiente.

El microservicio se conecta a las OLTs utilizando SNMP (Simple Network Management Protocol) mediante la librería gosnmp, asi como telnet en algunos casos permitiendo extraer datos como (Hora del equipo, Temperatura y estado de los componentes, Uptime, Información detallada de las ONUs: tráfico, Estatus, SN, Hostname, potencia de señal, etc.) esto utilizando goroutines para ejecutar tareas en segundo plano sin bloquear el hilo principal de la aplicación.

🛠 Tecnologías utilizadas

* Arquitectura Limpia y Modular: El proyecto no es un único archivo monolítico. La estructura está claramente separada en paquetes con responsabilidades definidas (app, controllers, middlewares). Esto con el objetivo de diseñar un sistema que sea fácil de mantener, testear y escalar a medida que crecen en complejidad. La inicialización de la aplicación (init) centraliza la configuración (variables de entorno, bases de datos, tareas programadas), manteniendo el main limpio y enfocado en arrancar el servidor.
* Observabilidad y Logging de Producción: Se Integro lumberjack para una gestión de logs profesional. Esto incluye rotación automática de archivos por tamaño, compresión de logs antiguos y gestión de backups. Esta es una característica crítica para cualquier aplicación en producción, ya que asegura que los logs no consuman todo el espacio en disco y facilita su análisis posterior. El middleware RequestLogger y la recuperación de panics (gin.RecoveryWithWriter) que escribe directamente al archivo de log, aseguran que cada petición y cada error inesperado queden registrados para su depuración.
* Configuración y Entornos: La configuración del modo de Gin (GIN_MODE) se extrae de variables de entorno. Esta es una práctica fundamental de The Twelve-Factor App que me permite desplegar el mismo artefacto en diferentes entornos (desarrollo, staging, producción) sin cambiar una sola línea de código.
* Manejo de Recursos y Concurrencia: El uso de defer para cerrar las conexiones a las bases de datos (PostgreSQL y MySQL) demuestra un manejo correcto de los recursos, previniendo fugas que podrían degradar el rendimiento del servicio a largo plazo. 
* API First y Documentación: El código incluye anotaciones de Swagger. Esto refleja una mentalidad de "API First", donde la documentación es parte integral del desarrollo. Un desarrollador senior sabe que una API sin documentación es una API incompleta. Esto facilita enormemente la integración con otros equipos (frontend, mobile, otros microservicios) y reduce la fricción en el ciclo de desarrollo.

En resumen, este proyecto no es solo "código que funciona". Es una demostración de cómo construir un servicio Go siguiendo las mejores prácticas de la industria: una arquitectura sólida, preparado para la observabilidad, con un manejo de errores robusto y una excelente experiencia para el desarrollador.

### this project contains the next tasks ###
* project to handle all olts related tasks
* get clock or time from olt (getClock, oltInfo, oltAutoWrite, oltCleaningDb, onuInfo, onuTraffic, onuCleaningDb)

### you need to install this packages using go ###
* go install github.com/githubnemo/CompileDaemon      # autoreload app on change
* go install github.com/swaggo/swag/cmd/swag@latest   # install in the OS swag command
* go get -u github.com/gin-gonic/gin                  # framework
* go get -u github.com/joho/godotenv                  # cargar variables de .env
* go get -u github.com/jackc/pgx/v5                   # postgresql driver
* go get -u github.com/jackc/pgx/v5/pgxpool           # postgresql driver
* go get -u github.com/go-sql-driver/mysql            # mysql driver
* go get -u gopkg.in/natefinch/lumberjack.v2          # logrotate
* go get -u github.com/go-co-op/gocron/v2             # crons
* go get -u github.com/swaggo/gin-swagger             # library to handle documentation on the project
* go get -u github.com/swaggo/files                   # library to handle documentation on the project
* go get -u github.com/gosnmp/gosnmp                  # library for extracting snmp use this for disable snmp debug (CompileDaemon -build="go build -tags gosnmp_nodebug -o olt" --command="./olt")
* go get -u github.com/prometheus-community/pro-bing  # library to handle ping operations just one at a time (single host), not multiple hosts

### you need also to create a .env file below are the related vars ### 

```
  PORT=7002

  # use release or debug mode, in debug: all request are logged with the header and body
  # also in debug mode is enable all string send to the olt via telnet are store in the log, as well as the response from telnet
  GIN_MODE=debug

  # postgres variables
  DB_POSTGRES=postgres://user:password@ip_address:port/database_name
  PGSQL_MAX_CONN=20
  PGSQL_MIN_CONN=3

  # mysql variables
  DB_MYSQL=user:password|@tcp(ip_address:port)/database_name
  MYSQL_MAX_CONN=4
  MYSQL_MIN_CONN=1

  # Variables to handle basic auth for access to api documentation url is /docs/index.html
  DOC_USER=username_here
  DOC_PASSWD=password_here

```

### Example of job definition: in .crontab ###
#### must create .crontab file on root folder of project to operate cron jobs, checkout crontab_example.json ####
```
 .---------------- minute (0 - 59)
 |  .------------- hour (0 - 23)
 |  |  .---------- day of month (1 - 31)
 |  |  |  .------- month (1 - 12) OR jan,feb,mar,apr ...
 |  |  |  |  .---- day of week (0 - 6) (Sunday=0 or 7) OR sun,mon,tue,wed,thu,fri,sat
 |  |  |  |  |
 *  *  *  *  * 
```


### create service using systemctl on linux
#### create file /etc/systemd/system/ired_olt.service

```
[Unit]
Description=web service, to handle olt related tasks runs on port 7002

[Install]
WantedBy=multi-user.target

[Service]
Type=simple
User=root
Restart=always
WorkingDirectory=/usr/local/src/ired.com/olt
ExecStart=/usr/local/src/ired.com/olt/olt
```