# User Policy Compiler

The User Defined Policy is written in a specific yaml format. An example Yaml File is listed below.

action: ALLOW
rules:
- name: test access

  condition: request.url_path.startsWith('/pkg.service/test')
- name: admin access

  condition: connection.uri_san_peer_certificate == 'clustes/ns/default/sa/admin'
- name: dev access

  condition: request.url_path == '/pkg.service/dev' &&
    connection.uri_san_peer_certificate == 'cluster/ns/default/sa/dev'
    
The first line starts by specifying the action purpose of the policy. 
action: ALLOW or DENY

The second line specifies that you will now begin listing the rules for your policy. 
  rules:
  Each rule follows the same format.
    A “- name:” to signify the role/access users abiding by this rule are given
    A tab then “condition:” to write out the CEL expressions you want evaluated
    More on how to write CEL expressions below
    If the CEL expression is too long it can be broken up onto the next line so long as the continuation of the statement is indented once like in the above example.
    

# What are the allowed fields one can create a policy using?

    request.url_path | string | The path portion of the URL in the form of "/packageName.serviceName/methodName".

    request.host | string | The host portion of the URL, e.g., "foo.example.com".

    request.method  | string | HTTP method, e.g., "GET", "POST".

    request.headers | string | map All request headers. 

    source.address | string | Connection remote address.

    source.port | int | Connection remote port.

    destination.address | string | Connection local address.

    destination.port | int | Connection local port.

    connection.uri_san_peer_certificate | string | First URI in SAN field of the peer certificate. In the Istio context, this is the SPIFFE ID.




# How to write an Expression 

For every condition you want to construct a statement involving the above approved fields that results in an outcome of True or False.
Use common logical operators such as and, or, less than, in, equals to construct statements involving the above attribute that your security policy depends on.
See the Language Definition for specific instruction in how to format expressions specifically to CEL.

# How to use the Compiler?
Start with accessing the directory of the grpc_authz_compiler.go which is within grpc-go/security/rbac/compiler/main

From there use:
 go run grpc_authz_compiler.go PATH_TO_INPUT_YAML OUTPUT_FILENAME

The output file will be produced in the ~/compiler/main directory
