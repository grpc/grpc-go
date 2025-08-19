Thank you for your PR.  Please follow the steps in this template to ensure a
swift review.

1. Read and follow the guidelines for contributing here:
   https://github.com/grpc/grpc-go/blob/master/CONTRIBUTING.md

   Note: if you are submitting a PR that does not address an open issue with an
   agreed resolution, it is much more likely your PR will be rejected.

2. Read and follow the guidelines for PR titles and descriptions here:
   https://google.github.io/eng-practices/review/developer/cl-descriptions.html

   *Particularly* the sections "First Line" and "Body is Informative".

   Note: your PR description will be used as the git commit message in a
   squash-and-merge if your PR is approved.  We may make changes to this as
   necessary.

3. PR titles should start with the name of the component being addressed, or the
   type of change.  Examples: transport, client, server, round_robin, xds,
   cleanup, deps.

4. Does this PR relate to an open issue?  On the first line, please use the tag
   `Fixes #<issue>` to ensure the issue is closed when the PR is merged.  Or use
   `Updates #<issue>` if the PR is related to an open issue, but does not fix
   it.  Consider filing an issue if one does not already exist.

5. PR descriptions *must* conclude with release notes as follows:

   ```
   RELEASE NOTES:
   * <component>: <summary>
   ```

   This need not match the PR title.

   The summary must:

   * be something that gRPC users will understand.

   * clearly explain the feature being added, the issue being fixed, or the
     behavior being changed, etc.  If fixing a bug, be clear about how the bug
     can be triggered by an end-user.

   * begin with a capital letter and use complete sentences.

   * be as short as possible to describe the change being made.

   If a PR is not end-user visible -- e.g. a cleanup, testing change, or
   github-related, use `RELEASE NOTES: n/a`.

6. Self-review your code changes before sending your PR.

7. All tests must be passing or you PR cannot be merged.  There are two github
   checkers that need not be green:

   1. We test the freshness of the generated proto code we maintain via the
      `vet-proto` check.  If the source proto files are updated, but our repo is
      not updated, an optional checker will fail.  This will be fixed by our
      team in a separate PR and will not prevent the merge of your PR.

   2. We run a checker that will fail if there is any change in dependencies of
      an exported package via the `dependencies` check.  If new dependencies are
      added that are not appropriate, we may not accept your PR.

8. Delete all of the above before sending your PR.
