# Overview

Went with a fully agentic build of the proxy leveraging openspec. Which allowed the base implementation of most of the requested features.

### Tech selection

The runtime and language of choice was golang since its lightweight and fast both to build and when it comes to running workloads. It also has the benefit of being agent friendly since with its type system and the fact there is typically only one clear way to do something. Rust would have been better for a fully agentic build and production performance but its resource hungry compliation would have prevent us from making significant progress in the 2hr timeframe.

Docker was chosen in since it makes orchestrating realistic tests an api gateway easy, not unlike what you would do running ambassador, nginx ect locally for dev.

### Process

I like to build agentially with a mentality of I am calling "hill climbing" for lack of a better word at the moment. Which is give the agents a target and then let them keep iterating until they git it.

So the first thing that was done was create a spec for the config file since our target was to produce software that would "run it". After we extracted the rest of the requirements and organized them into layers based on what needed to be built first. At this point the two documents were fed into openspec with some light custom rules on my end.

This produced a bunch of spec files which I reviewed to make sure they aligned with what I wanted to build (not my first time building a proxy/router so I went on gut instinct due to time constraint). The goal of the specs was not only to implement the behaviors but to do it with as much parallelisation in the execution as possible (this leads to some unwanted effects in general around architecture which we will dicuss later on).

The specs were implementing using the openspec apply skill but executed across as many claude code instances as needed with an orchestrator agent driving the entire build.

After the core build was complete a few workflows that are not in the spec were kicked off.

- running fuzzing tests of the config yaml against the running docker container instance of the gatway and its upstreams
- removing the hand rolled yaml parser in favor of a library
- Some light codebase clean up

Left out due to time is cleaning up the effects of a parallel build on the codebase which are some code repetition and the codebase not being structure fully how you would expect a golang project to be. Typically I would keep polishing for longer before I would say this is ready for a review but while agents allow for massive amounts of work to be done they don't like doing it in time constraints.

### What I would do with more time

Definitely polish the codebase first but also increase the production readiness of the system by doing the following:

- Adding instrumentation, OTEL.
- improving the rate limit and load balacing algos, they are pretty basic and wouldn't hold up in real life specially the rate limiting.
- Add documentation around operating the system and how to extend it for common scenarios.
- Test it running under more realstic load scenarios.
