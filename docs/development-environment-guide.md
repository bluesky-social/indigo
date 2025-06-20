# AT Protocol Development Environment for Canadian Data Sovereignty

## Overview: Understanding the Architecture

Welcome to your comprehensive AT Protocol development environment! This setup has been specifically designed to support Gander Social's mission of creating a data-sovereign Canadian social media platform while maintaining compatibility with the broader AT Protocol ecosystem.

Think of this setup as creating two parallel universes: one that operates in the global AT Protocol network, and another that maintains strict Canadian data sovereignty. Data can flow between these universes under controlled conditions, but Canadian user data always remains under Canadian jurisdiction.

## Core Infrastructure Components

### PostgreSQL Database: The Foundation of Data Persistence

PostgreSQL serves as our primary data store, providing ACID compliance and relational integrity that's crucial for identity management and user authentication. We've configured separate databases for each service to maintain clear boundaries and enable service-specific optimizations.

The database setup includes a special `canada_dev` database with enhanced compliance features. This database has row-level security enabled, which means we can implement fine-grained access controls at the database level. This is particularly important for Canadian privacy law compliance, where we need to ensure that Canadian user data can only be accessed by authorized Canadian services.

The compliance audit table we've created will track every data access operation, creating an tamper-evident audit trail. This is essential for demonstrating compliance with Canadian privacy laws like PIPEDA (Personal Information Protection and Electronic Documents Act).

### Redis Cache: High-Performance Session Management

Redis provides our in-memory data structure storage, which is essential for several critical functions. First, it handles session management - when Canadian users log in, their session data is stored in Redis with appropriate geographic tags. Second, it provides high-performance caching for frequently accessed data, reducing database load and improving response times.

We've configured separate Redis databases (numbered 0-6) for different services. This logical separation ensures that global and Canadian services don't accidentally share cached data, maintaining our sovereignty boundaries.

### IPFS Node: Decentralized Content Storage

IPFS (InterPlanetary File System) provides content-addressed storage, which means content is identified by its cryptographic hash rather than its location. This creates several advantages for our Canadian sovereignty goals:

Content stored in IPFS is inherently tamper-proof - if someone modifies it, the hash changes, making the modification detectable. For Canadian users, we can ensure their content is pinned to Canadian IPFS nodes, keeping the data within Canadian borders while still participating in the global content addressing system.

### Mock HSM: Secure Key Management Simulation

The Hardware Security Module (HSM) simulation provides secure key storage and cryptographic operations. In our development environment, this is a mock service, but in production, it would be replaced with actual HSM hardware or a cloud HSM service that meets Canadian security standards.

The HSM is crucial for our private PDS messaging requirements. When Canadian users send private messages through their Personal Data Servers, the HSM ensures that encryption keys are managed securely and that only authorized recipients can decrypt the messages. The HSM's audit trail also helps with compliance reporting.

## AT Protocol Services: The Social Network Foundation

### PLC Directory: Decentralized Identity Management

The PLC (Public Ledger of Credentials) manages DID (Decentralized Identifier) documents. Think of this as a phone book for the decentralized web - when someone wants to find a user's current data location or verify their identity, they consult the PLC.

For Canadian sovereignty, the PLC allows Canadian users to maintain their global identity while ensuring their data processing happens through Canadian services. A Canadian user's DID document will point to Canadian PDS and AppView services rather than global ones.

### Personal Data Servers (PDS): User Data Ownership

We've configured two PDS instances: one for global users and one specifically for Canadian users. The PDS is where each user's personal data actually lives - their posts, follows, blocks, and other social data.

The Canadian PDS (`pds_canada`) has several sovereignty-specific features:
- All data is tagged with jurisdiction markers
- Encryption is required for all stored data
- The HSM is integrated for key management
- Audit logging tracks all data access

This ensures that Canadian users' data never leaves Canadian jurisdiction and is always encrypted at rest.

### Big Graph Service (BGS): The Firehose Architecture

The BGS provides the "firehose" - a real-time stream of all public data in the network. This is where your dual firehose requirement comes into play.

We have two BGS instances:
1. `bgs_global` - connects to the global AT Protocol network
2. `bgs_canada` - provides a Canadian-sovereign firehose

The Canadian BGS only includes data from Canadian users who have explicitly authorized Gander Social to include them in the Canadian feed. This creates a sovereignty-compliant data stream that other Canadian services can subscribe to without worrying about cross-border data transfer issues.

### AppView: The Social Application Layer

AppView indexes and serves social graph data, providing the APIs that social media clients use. Again, we have two instances:

The Canadian AppView (`appview_canada`) only processes data from the Canadian firehose, ensuring that Canadian users' social graph data is only processed by Canadian services. It includes the same social features as the global AppView but with enhanced encryption and compliance tracking.

## Data Sovereignty Implementation

### Network Isolation Strategy

We've implemented two Docker networks:
- `atproto_network` - for global services with internet access
- `canada_network` - marked as internal, providing isolation for Canadian services

This network separation ensures that Canadian services can communicate with each other and with necessary global services (like the PLC for identity resolution), but Canadian user data cannot accidentally leak to external services.

### Encryption and Key Management

All Canadian services are configured to require encryption and use the HSM for key management. This means:
- Data at rest is encrypted using keys managed by the HSM
- Inter-service communication uses TLS with HSM-managed certificates
- Private messages use end-to-end encryption with keys that never leave the HSM

### Compliance and Audit Trail

Every Canadian service logs its operations to the compliance audit table. This creates a complete audit trail showing:
- What data was accessed
- By which service
- At what time
- For what purpose
- Under what jurisdiction

This audit trail is essential for demonstrating compliance with Canadian privacy laws and can be used to generate compliance reports for regulatory authorities.

## Development Workflows

### Starting the Environment

To start the complete development environment:

```bash
# Start all services
docker-compose -f docker/development.yml up -d

# Or start services incrementally for debugging
docker-compose -f docker/development.yml up -d postgres redis ipfs mock_hsm
docker-compose -f docker/development.yml up -d plc
docker-compose -f docker/development.yml up -d pds_global pds_canada
docker-compose -f docker/development.yml up -d bgs_global bgs_canada
docker-compose -f docker/development.yml up -d appview appview_canada
```

### Service URLs and Access Points

Once running, you can access various services:

**Core Infrastructure:**
- PostgreSQL: `localhost:5432` (user: gndr, password: yksb_dev_2024)
- Redis: `localhost:6379` (password: redis_dev_2024)
- IPFS API: `localhost:5001`
- Mock HSM: `localhost:8200`

**AT Protocol Services:**
- PLC Directory: `localhost:2582`
- Global PDS: `localhost:2583`
- Canadian PDS: `localhost:2585`
- Global BGS/Firehose: `localhost:2470`
- Canadian BGS/Firehose: `localhost:2471`
- Global AppView: `localhost:2584`
- Canadian AppView: `localhost:2586`

**Development Tools:**
- pgAdmin (Database Management): `localhost:8081`
- Redis Commander: `localhost:8082`
- IPFS Web UI: `localhost:8083`

### Testing Canadian Data Sovereignty

To verify that your Canadian sovereignty implementation is working:

1. **Create a Canadian User:**
   ```bash
   curl -X POST localhost:2585/xrpc/com.atproto.server.createAccount \
     -H "Content-Type: application/json" \
     -d '{"handle":"test.canada","email":"test@gander.social","password":"test123","jurisdiction":"CA"}'
   ```

2. **Verify Data Isolation:**
   Check that the user appears in the Canadian firehose but not the global one.

3. **Test Compliance Logging:**
   Query the compliance audit table to ensure all operations are being logged.

## Security Considerations for Production

While this development environment simulates production security, remember these considerations for your production deployment:

### HSM Selection
Replace the mock HSM with a real HSM that meets Canadian security standards. Consider options like:
- Cloud HSM services from Canadian cloud providers
- On-premises HSM hardware for maximum control
- FIPS 140-2 Level 3 certified devices for government compliance

### Network Security
Implement proper network segmentation in production:
- Use VPNs or private networks for inter-service communication
- Implement proper firewall rules
- Consider using service mesh technology like Istio for advanced traffic management

### Data Encryption
Ensure all data is encrypted both at rest and in transit:
- Use TLS 1.3 for all network communication
- Implement transparent database encryption
- Use envelope encryption for large data objects

### Monitoring and Alerting
Implement comprehensive monitoring:
- Real-time alerts for compliance violations
- Performance monitoring for all services
- Security event monitoring and response

## Integration with ThinkOn Infrastructure

Since ThinkOn is providing your hosting and relays, you'll need to adapt this development environment to work with their infrastructure. Key considerations:

1. **Network Configuration:** Ensure Canadian services can communicate with ThinkOn's Canadian data centers while maintaining sovereignty boundaries.

2. **Compliance Mapping:** Map ThinkOn's compliance certifications to your audit requirements.

3. **Backup and Recovery:** Implement Canadian-compliant backup procedures using ThinkOn's infrastructure.

4. **Scaling Strategy:** Plan how to scale Canadian services independently of global services as your user base grows.

## Next Steps for Implementation

1. **Week 1-2:** Get this development environment running and familiarize yourself with each component
2. **Week 3-4:** Implement Canadian user registration and data flow testing
3. **Week 5-6:** Develop compliance reporting and audit tools
4. **Week 7-8:** Begin integration testing with ThinkOn infrastructure
5. **Week 9-10:** Performance testing and optimization
6. **Week 11-12:** Security testing and compliance validation

This development environment provides a solid foundation for building Gander Social's Canadian data sovereignty features. The separation between global and Canadian services, combined with proper encryption and audit trails, ensures you can meet Canadian privacy law requirements while still participating in the broader AT Protocol ecosystem.

Remember, data sovereignty is not just about technical implementation - it's also about operational procedures, legal compliance, and ongoing governance. This technical foundation supports those broader requirements by providing the necessary isolation, encryption, and audit capabilities.
