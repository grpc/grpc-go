This package contains tests which cannot live in the `client` package because they need to import one of the API client packages (which itself has a dependency on the `client` package).
