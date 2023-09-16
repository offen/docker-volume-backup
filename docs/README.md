# Documentation site

This directory contains the sources for the documentation site published at <https://offen.github.io/docker-volume-backup>.

Assuming you have Ruby and [`bundler`][bundler] installed, you can run the site locally using the following commands:

```
bundle install
bundle exec jekyll serve
```

Note that changes in `_config.yml` require a manual restart to take effect.

[bundler]: https://bundler.io/
