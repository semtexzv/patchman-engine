# Openshift deployment templates
In addition to docker-compose, we support deployment to openshift. In order to allow customizing the deployment we use\
the `ocdeployer` tool.

Example deployment script which creates BuildConfig objects along with service and DeploymentConfig objects for this 
project:

```ocdeployer deploy -t openshift patchman-engine-ci -s deploy,build -e ./openshift/ci-env.yml```


## Conventions
Each object is prefixed by `spm-engine-` prefix. While we are running inside a dedicated namespace, the BuildConfig
definitions will be running in shared `buildfactory` namespace, and thus require a dedicated prefix. 
The prefix is applied to all objects, so we have consistent naming strategy.

