import * as React from 'react';
import Container from 'react-bootstrap/Container'
import {inject, observer} from "mobx-react";
import BranchStore from "stores/BranchStore";
import {BranchInfo} from "components/BranchInfo";
import Row from "react-bootstrap/Row";
import Col from "react-bootstrap/Col";
import Button from "react-bootstrap/Button";
import Popover from "react-bootstrap/Popover";
import OverlayTrigger from "react-bootstrap/OverlayTrigger";
import InputGroup from "react-bootstrap/InputGroup";
import FormControl from "react-bootstrap/FormControl";
import "styles/style.css";

interface Props {
    branchStore?: BranchStore;
}

@inject("branchStore")
@observer
export class BranchDAG extends React.Component<Props, any> {
    componentDidMount() {
        this.props.branchStore.start();
    }

    componentWillUnmount() {
        this.props.branchStore.stop();
    }

    pauseResumeVisualizer = (e) => {
        this.props.branchStore.pauseResume();
    }

    updateVerticesLimit = (e) => {
        this.props.branchStore.updateVerticesLimit(e.target.value);
    }

    updateSearch = (e) => {
        this.props.branchStore.updateSearch(e.target.value);
    }

    searchAndHighlight = (e: any) => {
        if (e.key !== 'Enter') return;
        this.props.branchStore.searchAndHighlight();
    }

    centerGraph = () => {
        this.props.branchStore.centerEntireGraph();
    }

    render () {
        let { paused, maxBranchVertices, search } = this.props.branchStore;

        return (
            <Container>
                <h2> Branch DAG </h2>
                <Row xs={5}>
                    <Col className="align-self-end" style={{display: "flex", justifyContent: "space-evenly"}}>
                        <InputGroup className="mb-1">
                            <OverlayTrigger
                                trigger={['hover', 'focus']} placement="right" overlay={
                                <Popover id="popover-basic">
                                    <Popover.Body>
                                        Pauses/resumes rendering the graph.
                                    </Popover.Body>
                                </Popover>}
                            >
                                <Button onClick={this.pauseResumeVisualizer} variant="outline-secondary">
                                    {paused ? "Resume Rendering" : "Pause Rendering"}
                                </Button>
                            </OverlayTrigger>
                        </InputGroup>
                        <InputGroup className="mb-1">
                            <Button onClick={this.centerGraph} variant="outline-secondary">
                                Center Graph
                            </Button>
                        </InputGroup>
                    </Col>
                    <Col>
                        <InputGroup className="mb-1">
                            <InputGroup.Text id="vertices-limit">Vertices Limit</InputGroup.Text>
                            <FormControl
                                placeholder="limit"
                                value={maxBranchVertices.toString()} onChange={this.updateVerticesLimit}
                                aria-label="vertices-limit"
                                aria-describedby="vertices-limit"
                            />
                        </InputGroup>
                    </Col>
                    <Col>
                        <InputGroup className="mb-1">
                            <InputGroup.Text id="search-vertices">
                                Search Vertex
                            </InputGroup.Text>
                            <FormControl
                                placeholder="search"
                                type="text" value={search} onChange={this.updateSearch}
                                aria-label="vertices-search" onKeyUp={this.searchAndHighlight}
                                aria-describedby="vertices-search"
                            />
                        </InputGroup>
                    </Col>                  
                </Row>
                <div className="graphFrame">
                    <BranchInfo />
                    <div id="branchVisualizer" />
                </div> 
            </Container>
            
        );
    }

}