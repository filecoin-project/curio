Curio exists to serve the Filecoin ecosystem: specifically the needs of Storage Providers.

__Principle__
Its design follows one principle: build only what a consortium of SPs would intentionally create for themselves on top of the Filecoin Node APIs.

__Fragmentation__
A fragmented experience is a risk whenever:
- central tools support some features but not others
- tools compose poorly
- learning curves prevent full suite rollout
To keep Filecoin SPs moving, Curio handles centralized needs, plus non-central matters that can accomplish entirely. For non-central matters it cannot accomplish entirely, APIs & connectors are provided (ex: custom UI, metrics, and notifications).

Curio Storage (team) intends to bring all of the best Filecoin tooling either into Curio or as a connector.

__Configuration__
Every configuration bring complexity, learning curve, support difficulties, and expectation breakages. 

To protect interoperability and avoid ecosystem fragmentation, Curio avoids arbitrary configuration that deviates from common standards. 

Therefore we allow them judiciously:
Configuration should:
- Enable SPs the choice to fit the tool to their unique business
- Offer a way to enable what an SP can allow, afford, and participate in.
- Enable conveniences that benefit that business' workflow.