# kctl

kctl is a tool to help make Kubernetes (kubectl) easier to use.

Usage:
  kctl [args]

kctl with no arguments will show this help message. Using --help will show
kubectl help. Generally kctl is aliased as "k" and the rest of the documentation
will reflect this. k proxies all arguments through to kubectl with a few
exceptions where it attempts to augment the arguments passed in to provide
an easier user interface.

There are 2 augmentations that k has over regular kubectl:

1. pattern matching

     When trying to find a service/pod, you can use a colon to denote a "search".
     On the left hand side of the colon is a namespace pattern and on the right
     is a resource pattern. The pattern syntax is described here:
     https://github.com/google/re2/wiki/Syntax

     The best way to understand the use of this is to see some examples

     ```bash
     k get pods :             # Get pods in --all-namespaces
     k get pods default:      # Get pods in a namespace matching "default"
     k describe pod f:^api$   # Describe the pod matching the patterns
     k exec 'f:^api.*' ps afx # Run ps afx on the pod matched by "f:^api$"
     k get services 'def.*:'  # Get all services in a namespace matching "default"
    ```

2. ssh command

   The SSH command allows you to easily start a bash session inside a pod
   and carries over your environment's terminal settings so that width/height
   and terminal type are set correctly for the new bash session.

   Example:

   ```bash
   k ssh default:podname
   ```
