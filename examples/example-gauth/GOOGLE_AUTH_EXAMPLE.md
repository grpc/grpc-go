Google Authentication Example
==============================================

This example illustrates how to access [Google APIs](https://cloud.google.com/apis/docs/overview) via gRPC and
open source [Google API libraries](https://github.com/googleapis) using
[Google authentication](https://developers.google.com/identity/protocols/OAuth2), specifically the
[GoogleCredentials](https://github.com/googleapis/google-auth-library-java/blob/master/oauth2_http/java/com/google/auth/oauth2/GoogleCredentials.java)
class.

The example requires grpc-java to be pre-built. Using a release tag will download the relevant binaries
from a maven repository. But if you need the latest SNAPSHOT binaries you will need to follow
[COMPILING](../COMPILING.md) to build these.

Please follow the [steps](./README.md#to-build-the-examples) to build the examples. The build creates
the script `google-auth-client` in the `build/install/examples/bin/` directory which can be
used to run this example.

The example uses [Google PubSub gRPC API](https://cloud.google.com/pubsub/docs/reference/rpc/) to get a list
of PubSub topics for a project. You will need to perform the following steps to get the example to work.
Wherever possible, the required UI links or `gcloud` shell commands are mentioned.

1. Create or use an existing [Google Cloud](https://cloud.google.com) account. In your account, you may need
to enable features to exercise this example and this may cost some money.

2. Use an existing project, or [create a project](https://pantheon.corp.google.com/projectcreate),
say `Google Auth Pubsub example`. Note down the project ID of this project - say `xyz123` for this example.
Use the project drop-down from the top or use the cloud shell command
```
gcloud projects list
```
to get the project ID.

3. Unless already enabled, [enable the Cloud Pub/Sub API for your project](https://console.developers.google.com/apis/api/pubsub.googleapis.com/overview)
by clicking `Enable`.

4. Go to the [GCP Pub/Sub console](https://pantheon.corp.google.com/cloudpubsub). Create a couple of new
topics using the "+ CREATE TOPIC" button, say `Topic1` and `Topic2`. You can also use the gcloud command
to create a topic:
```
gcloud pubsub topics create Topic1
```

5. You will now need to set up [authentication](https://cloud.google.com/docs/authentication/) and a
[service account](https://cloud.google.com/docs/authentication/#service_accounts) in order to access
Pub/Sub via gRPC APIs as described [here](https://cloud.google.com/iam/docs/creating-managing-service-accounts).
Assign the [role](https://cloud.google.com/iam/docs/granting-roles-to-service-accounts) `Project -> Owner`
and for Key type select JSON. Once you click `Create`, a JSON file containing your key is downloaded to
your computer. Note down the path of this file or copy this file to the computer and file system where
you will be running the example application as described later. Assume this JSON file is available at
`/path/to/JSON/file`. You can also use the `gcloud` shell commands to
[create the service account](https://cloud.google.com/iam/docs/creating-managing-service-accounts#iam-service-accounts-create-gcloud)
and [the JSON file](https://cloud.google.com/iam/docs/creating-managing-service-account-keys#iam-service-account-keys-create-gcloud).

#### To build the examples

1. **[Install gRPC Java library SNAPSHOT locally, including code generation plugin](../../COMPILING.md) (Only need this step for non-released versions, e.g. master HEAD).**

2. Run in this directory:
```
$ ../gradlew installDist
```


#### How to run the example:
`google-auth-client` requires two command line arguments for the location of the JSON file and the project ID:

 ```text
USAGE: GoogleAuthClient <path-to-JSON-file> <project-ID>
```

The first argument <path-to-JSON-file> is the location of the JSON file you created in step 5 above.
The second argument <project-ID> is the project ID in the form "projects/xyz123" where "xyz123" is
the project ID of the project you created (or used) in step 2 above.

 ```bash
# Run the client
./build/install/example-gauth/bin/google-auth-client /path/to/JSON/file projects/xyz123
```
 That's it! The client will show the list of Pub/Sub topics for the project as follows:

 ```
 INFO: Topics list:
 [name: "projects/xyz123/topics/Topic1"
 , name: "projects/xyz123/topics/Topic2"
 ]
 ```

 ## Maven
 If you prefer to use Maven:
 1. **[Install gRPC Java library SNAPSHOT locally, including code generation plugin](../../COMPILING.md) (Only need this step for non-released versions, e.g. master HEAD).**

 2. Run in this directory:
 ```
 $ mvn verify
 $ # Run the client
 $ mvn exec:java -Dexec.mainClass=io.grpc.examples.googleAuth.GoogleAuthClient -Dexec.args="/path/to/JSON/file projects/xyz123"
 ```

 ## Bazel
 If you prefer to use Bazel:
 ```
 $ bazel build :google-auth-client
 $ # Run the client
 $ ../bazel-bin/google-auth-client /path/to/JSON/file projects/xyz123
 ```